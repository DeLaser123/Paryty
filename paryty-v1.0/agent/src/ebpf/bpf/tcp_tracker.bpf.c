// SPDX-License-Identifier: GPL-2.0
/*
 * tcp_tracker.bpf.c — TCP connection tracking via kprobes.
 *
 * Hooks:
 *   kprobe/tcp_connect       — capture outgoing SYN (store partial event)
 *   kretprobe/tcp_v4_connect — correlate success/failure, emit EVENT_TCP_CONNECT
 *   kprobe/tcp_set_state     — emit EVENT_TCP_ACCEPT / EVENT_TCP_CLOSE on
 *                              state transitions
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

/* Ring buffer for streaming events to userspace. */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RING_BUFFER_SIZE);
} tcp_events SEC(".maps");

/*
 * Correlation map: keyed by tid (pid_tgid), holds the partially-filled
 * tcp_event between kprobe/tcp_connect and kretprobe/tcp_v4_connect.
 * Entries are short-lived (microseconds).
 */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);
    __type(value, struct tcp_event);
} connect_info SEC(".maps");

/* ────────────────────────── Helper ────────────────────────── */

/*
 * emit_event — reserve space in the ring buffer, copy the event, submit.
 *
 * Returns 0 on success, negative on failure (reservation failed).
 * The verifier needs the memcpy size to be a compile-time constant.
 */
static __always_inline int emit_event(struct tcp_event *evt)
{
    struct tcp_event *ring_evt;

    ring_evt = bpf_ringbuf_reserve(&tcp_events, sizeof(struct tcp_event), 0);
    if (!ring_evt)
        return -1;

    __builtin_memcpy(ring_evt, evt, sizeof(struct tcp_event));
    bpf_ringbuf_submit(ring_evt, 0);
    return 0;
}

/* ────────────────────────── Programs ──────────────────────── */

/*
 * kprobe/tcp_connect — fires when the kernel initiates a TCP connection.
 *
 * Probe signature: int tcp_connect(struct sock *sk)
 *
 * We stash the 4-tuple + metadata in connect_info so the kretprobe can
 * correlate and emit on success.
 */
SEC("kprobe/tcp_connect")
int BPF_KPROBE(handle_tcp_connect, struct sock *sk)
{
    struct tcp_event evt = {};
    __u64 tid;
    __u32 saddr, daddr;
    __u16 sport, dport;

    /* pid_tgid: upper 32 bits = pid, lower 32 = tid */
    tid = bpf_get_current_pid_tgid();
    evt.pid = (__u32)(tid >> 32);
    evt.tid = (__u32)tid;

    /* Read the 4-tuple from struct sock via CO-RE */
    saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    dport = BPF_CORE_READ(sk, __sk_common.skc_dport);

    evt.src_ip   = saddr;
    evt.dst_ip   = daddr;
    evt.src_port = sport;
    evt.dst_port = bpf_ntohs(dport);

    evt.timestamp_ns = bpf_ktime_get_ns();
    bpf_get_current_comm(evt.comm, sizeof(evt.comm));

    /* Store for the kretprobe to pick up */
    bpf_map_update_elem(&connect_info, &tid, &evt, BPF_ANY);

    return 0;
}

/*
 * kretprobe/tcp_v4_connect — fires on return from tcp_v4_connect.
 *
 * Probe signature: int tcp_v4_connect(struct sock *sk)
 * Return value (ctx->ax on x86_64) indicates success (0) or error.
 *
 * On success we finalise the event and push it into the ring buffer.
 */
SEC("kretprobe/tcp_v4_connect")
int BPF_KRETPROBE(handle_tcp_v4_connect_ret, int ret)
{
    __u64 tid;
    struct tcp_event *evt;

    tid = bpf_get_current_pid_tgid();

    evt = bpf_map_lookup_elem(&connect_info, &tid);
    if (!evt)
        return 0;   /* kprobe did not fire (filtered or missed) */

    if (ret == 0) {
        evt->tcp_state  = TCP_ESTABLISHED;
        evt->event_type = EVENT_TCP_CONNECT;
        evt->timestamp_ns = bpf_ktime_get_ns();
        emit_event(evt);
    }

    /* Always clean up — prevents map leaks on error paths */
    bpf_map_delete_elem(&connect_info, &tid);

    return 0;
}

/*
 * kprobe/tcp_set_state — fires on every TCP state transition.
 *
 * Probe signature: void tcp_set_state(struct sock *sk, int state)
 *
 * We emit:
 *   EVENT_TCP_ACCEPT  when a passive socket becomes ESTABLISHED
 *   EVENT_TCP_CLOSE   when a connection enters TIME_WAIT or CLOSE
 */
SEC("kprobe/tcp_set_state")
int BPF_KPROBE(handle_tcp_set_state, struct sock *sk, int state)
{
    struct tcp_event evt = {};
    __u64 tid;
    __u32 saddr, daddr;
    __u16 sport, dport;

    /* We only care about a handful of terminal / accepted states */
    if (state != TCP_ESTABLISHED &&
        state != TCP_TIME_WAIT  &&
        state != TCP_CLOSE)
    {
        return 0;
    }

    tid = bpf_get_current_pid_tgid();
    evt.pid = (__u32)(tid >> 32);
    evt.tid = (__u32)tid;

    /* Read the 4-tuple */
    saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
    sport = BPF_CORE_READ(sk, __sk_common.skc_num);
    dport = BPF_CORE_READ(sk, __sk_common.skc_dport);

    evt.src_ip   = saddr;
    evt.dst_ip   = daddr;
    evt.src_port = sport;
    evt.dst_port = bpf_ntohs(dport);

    evt.tcp_state    = (__u32)state;
    evt.timestamp_ns = bpf_ktime_get_ns();
    bpf_get_current_comm(evt.comm, sizeof(evt.comm));

    if (state == TCP_ESTABLISHED)
        evt.event_type = EVENT_TCP_ACCEPT;
    else
        evt.event_type = EVENT_TCP_CLOSE;

    emit_event(&evt);

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
