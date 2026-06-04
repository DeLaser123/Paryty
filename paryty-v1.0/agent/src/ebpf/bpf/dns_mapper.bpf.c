// SPDX-License-Identifier: GPL-2.0
/*
 * dns_mapper.bpf.c — DNS query interception via kprobe on udp_sendmsg.
 *
 * Captures outgoing UDP packets to port 53 and emits a dns_event with
 * metadata (src/dst IP, ports).  Full DNS parsing is deferred to userspace
 * since the raw DNS payload can exceed what the verifier comfortably
 * allows us to parse in-kernel.
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
} dns_events SEC(".maps");

/* ────────────────────────── Constants ─────────────────────── */

#define DNS_PORT 53

/* Placeholder domain — full parsing in userspace */
static const char pending_domain[] = "<pending>";

/* ────────────────────────── Helper ────────────────────────── */

/*
 * copy_pending — copy the literal "<pending>" into dst.
 *
 * The verifier needs the loop bound to be a compile-time constant.
 * "<pending>" is 10 bytes including the NUL (9 chars + '\0').
 */
static __always_inline void copy_pending(char *dst, int dst_len)
{
    int i;
    #pragma unroll
    for (i = 0; i < 10 && i < dst_len; i++)
        dst[i] = pending_domain[i];
}

/* ────────────────────────── Programs ──────────────────────── */

/*
 * kprobe/udp_sendmsg — fires for every outgoing UDP datagram.
 *
 * Probe signature:
 *   int udp_sendmsg(struct sock *sk, struct msghdr *msg, size_t len)
 *
 * We filter on destination port == 53 (DNS) and emit a dns_event.
 */
SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(handle_udp_sendmsg, struct sock *sk,
               struct msghdr *msg, size_t len)
{
    struct dns_event evt = {};
    __u64 tid;
    __u32 saddr, daddr;
    __u16 sport, dport;

    /* Only capture DNS queries (dst port 53) */
    dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    if (dport != DNS_PORT)
        return 0;

    /* Populate event header */
    tid = bpf_get_current_pid_tgid();
    evt.pid = (__u32)(tid >> 32);

    saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    sport = BPF_CORE_READ(sk, __sk_common.skc_num);

    evt.timestamp_ns = bpf_ktime_get_ns();
    evt.event_type   = EVENT_DNS_QUERY;
    evt.src_ip       = saddr;
    evt.dst_ip       = daddr;
    evt.src_port     = sport;
    evt.dst_port     = dport;
    evt.query_id     = 0;   /* parsed from payload in userspace */
    evt.qtype        = 0;   /* parsed from payload in userspace */

    /* Domain placeholder — userspace extracts from captured payload */
    copy_pending(evt.domain, MAX_DOMAIN_LEN);

    /* Emit via ring buffer */
    struct dns_event *ring_evt;

    ring_evt = bpf_ringbuf_reserve(&dns_events, sizeof(struct dns_event), 0);
    if (!ring_evt)
        return 0;

    __builtin_memcpy(ring_evt, &evt, sizeof(struct dns_event));
    bpf_ringbuf_submit(ring_evt, 0);

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
