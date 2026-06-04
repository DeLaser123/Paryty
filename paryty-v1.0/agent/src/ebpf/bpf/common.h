/* SPDX-License-Identifier: GPL-2.0 */
/*
 * common.h — Shared event structures for Paryty eBPF Network Observer.
 *
 * These structs are exchanged between BPF kernel programs and Rust userspace
 * via BPF ring buffers.  The Rust side must use #[repr(C)] with identical
 * field ordering and types so that zero-copy parsing is safe.
 */
#ifndef __PARYTY_BPF_COMMON_H
#define __PARYTY_BPF_COMMON_H

/* ────────────────────────── Limits ────────────────────────── */
#define MAX_COMM_LEN        16
#define MAX_DOMAIN_LEN      256
#define MAX_HTTP_PATH_LEN   128
#define MAX_HTTP_HOST_LEN   128

/* ────────────────────────── Ring buffer ───────────────────── */
#define RING_BUFFER_SIZE    (256 * 1024)   /* 256 KiB — must be power of 2 */

/* ────────────────────────── Event types ───────────────────── */
enum event_type {
    EVENT_TCP_CONNECT  = 1,
    EVENT_TCP_ACCEPT   = 2,
    EVENT_TCP_CLOSE    = 3,
    EVENT_DNS_QUERY    = 4,
    EVENT_HTTP_REQUEST = 5,
    EVENT_HTTPS_SNI    = 6,
};

/* ────────────────────────── TCP states (mirror kernel) ────── */
#define TCP_ESTABLISHED   1
#define TCP_SYN_SENT      2
#define TCP_FIN_WAIT1     4
#define TCP_FIN_WAIT2     5
#define TCP_TIME_WAIT     6
#define TCP_CLOSE_WAIT    7
#define TCP_CLOSE         7   /* note: same value as CLOSE_WAIT per task spec */

/* ────────────────────────── Event structs ─────────────────── */

/*
 * struct tcp_event — TCP connection lifecycle events.
 *
 * Field ordering chosen so that natural alignment produces no padding holes:
 *   u64 (8) | u32 u32 (8) | u32 u32 (8) | u16 u16 (4) | u32 (4) | u64 u64 (16) | char[16]
 * Total: 64 bytes, packed.
 */
struct tcp_event {
    __u64 timestamp_ns;
    __u32 event_type;
    __u32 pid;
    __u32 tid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 tcp_state;
    __u64 bytes_sent;
    __u64 bytes_recv;
    char  comm[MAX_COMM_LEN];
} __attribute__((packed));

/*
 * struct dns_event — DNS query interception events.
 */
struct dns_event {
    __u64 timestamp_ns;
    __u32 event_type;
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u16 query_id;
    __u16 qtype;
    char  domain[MAX_DOMAIN_LEN];
} __attribute__((packed));

/*
 * struct http_event — HTTP request / HTTPS SNI detection events.
 */
struct http_event {
    __u64 timestamp_ns;
    __u32 event_type;
    __u32 pid;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u16 status_code;
    __u8  method;
    __u8  _pad;
    char  host[MAX_HTTP_HOST_LEN];
    char  path[MAX_HTTP_PATH_LEN];
} __attribute__((packed));

#endif /* __PARYTY_BPF_COMMON_H */
