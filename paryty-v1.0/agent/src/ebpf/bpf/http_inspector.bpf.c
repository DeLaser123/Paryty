// SPDX-License-Identifier: GPL-2.0
/*
 * http_inspector.bpf.c — HTTP request / HTTPS SNI detection.
 *
 * Hooks kprobe/tcp_sendmsg to inspect outgoing TCP traffic on well-known
 * HTTP and HTTPS ports.  Full payload inspection (method, path, SNI) is
 * done in userspace; the BPF side emits lightweight metadata events so
 * the userspace consumer knows which flows to inspect.
 */

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#ifndef __TARGET_ARCH_x86
#define __TARGET_ARCH_x86
#endif
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include "common.h"

/* ────────────────────────── Maps ──────────────────────────── */

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RING_BUFFER_SIZE);
} http_events SEC(".maps");

/* ────────────────────────── Port helpers ──────────────────── */

/*
 * is_http_port — returns 1 if the port is a common plaintext HTTP port.
 */
static __always_inline int is_http_port(__u16 port)
{
    return (port == 80) || (port == 8080) || (port == 8000) ||
           (port == 3000) || (port == 5000);
}

/*
 * is_https_port — returns 1 if the port is a common TLS port.
 */
static __always_inline int is_https_port(__u16 port)
{
    return (port == 443) || (port == 8443);
}

/* ────────────────────────── Programs ──────────────────────── */

/*
 * kprobe/tcp_sendmsg — fires for every outgoing TCP message.
 *
 * Probe signature:
 *   int tcp_sendmsg(struct sock *sk, struct msghdr *msg, size_t size)
 *
 * We filter on destination port being HTTP or HTTPS, then emit an
 * http_event with metadata.  Actual payload parsing (method, path, SNI)
 * is handled by the Rust userspace consumer reading a small snapshot
 * of the payload from a separate mechanism (or perf event in Phase 3+).
 */
SEC("kprobe/tcp_sendmsg")
int BPF_KPROBE(handle_tcp_sendmsg, struct sock *sk,
               struct msghdr *msg, size_t size)
{
    struct http_event evt = {};
    __u64 tid;
    __u32 saddr, daddr;
    __u16 sport, dport;
    int is_http, is_https;

    /* Read destination port to decide if we care about this flow */
    dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    is_http  = is_http_port(dport);
    is_https = is_https_port(dport);

    if (!is_http && !is_https)
        return 0;

    /* Populate event */
    tid = bpf_get_current_pid_tgid();
    evt.pid = (__u32)(tid >> 32);

    saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    sport = BPF_CORE_READ(sk, __sk_common.skc_num);

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.event_type   = is_https ? EVENT_HTTPS_SNI : EVENT_HTTP_REQUEST;
    evt.src_ip       = saddr;
    evt.dst_ip       = daddr;
    evt.src_port     = sport;
    evt.dst_port     = dport;
    evt.status_code  = 0;   /* request-side; response status set later */
    evt.method       = 0;   /* parsed in userspace */
    evt._pad         = 0;

    /*
     * host[] and path[] are left zeroed here.  The userspace consumer
     * will read them from the payload buffer.  Attempting to parse
     * HTTP headers in eBPF would be complex, fragile, and likely hit
     * verifier instruction limits.
     */

    /* Emit via ring buffer */
    struct http_event *ring_evt;

    ring_evt = bpf_ringbuf_reserve(&http_events, sizeof(struct http_event), 0);
    if (!ring_evt)
        return 0;

    __builtin_memcpy(ring_evt, &evt, sizeof(struct http_event));
    bpf_ringbuf_submit(ring_evt, 0);

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
