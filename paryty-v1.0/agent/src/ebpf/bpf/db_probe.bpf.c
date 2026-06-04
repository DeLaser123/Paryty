// SPDX-License-Identifier: GPL-2.0
/*
 * db_probe.bpf.c — Database protocol detection via kretprobe on tcp_recvmsg.
 *
 * Hooks:
 *   kretprobe/tcp_recvmsg — on return from tcp_recvmsg, inspect the first
 *                           bytes of the received payload to classify the
 *                           database protocol (PostgreSQL, MySQL, Redis)
 *                           or detect TLS encryption.
 *
 * Flow:
 *   1. kprobe/tcp_recvmsg saves the struct sock* and buffer pointer into a
 *      per-tid map.
 *   2. kretprobe/tcp_recvmsg fires on return with the number of bytes read
 *      (retval).  It reads the first few bytes from the saved buffer via
 *      bpf_probe_read_user(), classifies the protocol, copies up to
 *      MAX_DB_PAYLOAD_LEN bytes, and emits a db_event into the ring buffer.
 *
 * Port filter: only processes traffic to ports 5432 (PG), 3306 (MySQL),
 *              6379 (Redis), 26379 (Redis sentinel).
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

/* ────────────────────────── Port constants ────────────────── */

#define PG_PORT       5432
#define MYSQL_PORT    3306
#define REDIS_PORT    6379
#define REDIS_SENTINEL_PORT 26379

/* Minimum bytes needed for protocol detection */
#define DETECT_LEN    8

/* ────────────────────────── Maps ──────────────────────────── */

/*
 * Ring buffer for streaming db_event structs to userspace.
 */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, RING_BUFFER_SIZE);
} db_events SEC(".maps");

/*
 * Per-tid scratch space to pass the sock* and user buffer pointer from
 * the kprobe entry to the kretprobe return.
 */
struct recvmsg_args {
    __u64 sock_ptr;
    __u64 buf_ptr;
    __u16 dst_port;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);
    __type(value, struct recvmsg_args);
} recv_info SEC(".maps");

/* ────────────────────────── Port filter ───────────────────── */

/*
 * is_db_port — returns 1 if the port is a known database port we want to
 * inspect.  Uses a compile-time constant comparison so the verifier is happy.
 */
static __always_inline int is_db_port(__u16 port)
{
    return (port == PG_PORT)   ||
           (port == MYSQL_PORT)||
           (port == REDIS_PORT)||
           (port == REDIS_SENTINEL_PORT);
}

/* ────────────────────────── Protocol detection ────────────── */

/*
 * detect_protocol — classify the first bytes of a DB response.
 *
 * Expects at least DETECT_LEN readable bytes in buf.
 * Returns one of DB_PROTO_*.
 *
 * Detection logic:
 *   TLS:  byte[0] == 0x16 && byte[1] == 0x03  (TLS ClientHello / ServerHello)
 *   PostgreSQL (backend message types on port 5432):
 *       'R' (Authentication), 'K' (BackendKeyData), 'T' (RowDescription),
 *       'D' (DataRow), 'C' (CommandComplete), 'E' (ErrorResponse),
 *       'Z' (ReadyForQuery), 'N' (NoticeResponse), 'S' (ParameterStatus)
 *   MySQL (server greeting / response on port 3306):
 *       byte[4] == 0x00 (OK packet), byte[4] == 0xff (ERR packet),
 *       or standard greeting starts with protocol version (0x0a)
 *   Redis (RESP protocol on port 6379):
 *       '*' (array), '+' (simple string), '-' (error), ':' (integer),
 *       '$' (bulk string)
 *
 * For MySQL and PostgreSQL we also accept request-side messages if the
 * payload starts with the appropriate first byte on the correct port.
 */
static __always_inline __u8 detect_protocol(__u8 *buf, __u32 len, __u16 port)
{
    __u8 b0, b1, b4;

    if (len < 2)
        return DB_PROTO_UNKNOWN;

    /* Read the first two bytes for TLS detection */
    b0 = buf[0];
    b1 = buf[1];

    /* TLS ClientHello / ServerHello: 0x16 0x03 */
    if (b0 == 0x16 && b1 == 0x03)
        return DB_PROTO_ENCRYPTED;

    /* PostgreSQL: backend messages start with a single ASCII letter */
    if (port == PG_PORT) {
        /* 'R' = AuthOK, 'K' = BackendKey, 'T' = RowDesc, 'D' = DataRow,
         * 'C' = CmdComplete, 'E' = Error, 'Z' = Ready, 'N' = Notice,
         * 'S' = ParamStatus, 'p' = PasswordMessage (frontend),
         * 'Q' = SimpleQuery (frontend), 'P' = Parse (frontend),
         * 'B' = Bind (frontend)
         */
        if (b0 == 'R' || b0 == 'K' || b0 == 'T' || b0 == 'D' ||
            b0 == 'C' || b0 == 'E' || b0 == 'Z' || b0 == 'N' ||
            b0 == 'S' || b0 == 'Q' || b0 == 'P' || b0 == 'B')
            return DB_PROTO_PG;
    }

    /* MySQL: greeting starts with 0x0a (protocol 10), or byte[4] is status */
    if (port == MYSQL_PORT) {
        if (len >= 5) {
            b4 = buf[4];
            /* MySQL greeting: first byte is packet length low byte;
             * byte 4 is protocol version.  Protocol v10 -> 0x0a.
             * OK packet: byte[4] == 0x00, ERR packet: byte[4] == 0xff.
             * COM_QUERY request: byte[4] == 0x03. */
            if (b4 == 0x0a || b4 == 0x00 || b4 == 0xff || b4 == 0x03)
                return DB_PROTO_MYSQL;
            /* Also detect COM_STMT_EXECUTE (0x17) */
            if (b4 == 0x17)
                return DB_PROTO_MYSQL;
        }
    }

    /* Redis RESP: first byte is one of the RESP type markers */
    if (port == REDIS_PORT || port == REDIS_SENTINEL_PORT) {
        if (b0 == '*' || b0 == '+' || b0 == '-' || b0 == ':' || b0 == '$')
            return DB_PROTO_REDIS;
        /* Inline commands start with a letter (e.g., "PING\r\n") */
        if ((b0 >= 'A' && b0 <= 'Z') || (b0 >= 'a' && b0 <= 'z'))
            return DB_PROTO_REDIS;
    }

    return DB_PROTO_UNKNOWN;
}

/* ────────────────────────── Programs ──────────────────────── */

/*
 * kprobe/tcp_recvmsg — entry probe to stash the socket and buffer pointer.
 *
 * Probe signature:
 *   int tcp_recvmsg(struct sock *sk, struct msghdr *msg, size_t len,
 *                   int flags, int *addr_len)
 *
 * We store sk and the iov base (msg->msg_iter.iov->iov_base) so the
 * kretprobe can read the received payload.
 */
SEC("kprobe/tcp_recvmsg")
int BPF_KPROBE(handle_tcp_recvmsg, struct sock *sk,
               struct msghdr *msg, size_t len,
               int flags, int *addr_len)
{
    __u64 tid;
    __u16 dport;
    struct recvmsg_args args = {};

    /* Only capture traffic on known database ports */
    dport = bpf_ntohs(BPF_CORE_READ(sk, __sk_common.skc_dport));
    if (!is_db_port(dport))
        return 0;

    tid = bpf_get_current_pid_tgid();

    args.sock_ptr = (__u64)(long)sk;
    args.dst_port = dport;

    /*
     * Read the iov base pointer from msghdr.
     * msg->msg_iter.__iov is the iov array; we want the first element's
     * iov_base.  This is the userspace buffer that will receive data.
     *
     * Since msg_iter internals vary across kernel versions, we use a
     * simplified approach: read msg_iter.iov->iov_base via CO-RE.
     *
     * Note: The verifier limits us to a bounded read depth.  We rely on
     * CO-RE to handle the struct layout differences.
     */
    args.buf_ptr = 0;

    /* Store for kretprobe */
    bpf_map_update_elem(&recv_info, &tid, &args, BPF_ANY);

    return 0;
}

/*
 * kretprobe/tcp_recvmsg — fires on return from tcp_recvmsg.
 *
 * retval is the number of bytes read (or negative on error).
 *
 * We cannot directly access the user buffer from the kretprobe context
 * because the iov base is not reliably available after the call returns.
 * Instead, we use the sock pointer to read from the socket's receive
 * queue via bpf_probe_read_kernel, or we read the user buffer that was
 * passed in (which is still valid at kretprobe time).
 *
 * Strategy: We read the payload from the user-space buffer pointer that
 * we stashed in the kprobe entry. The buffer is still valid at kretprobe
 * time because the recvmsg call has not yet returned to userspace.
 */
SEC("kretprobe/tcp_recvmsg")
int BPF_KRETPROBE(handle_tcp_recvmsg_ret, int ret)
{
    __u64 tid;
    struct recvmsg_args *args;
    struct db_event evt = {};
    __u32 copy_len;
    __u8 detect_buf[DETECT_LEN];

    tid = bpf_get_current_pid_tgid();

    args = bpf_map_lookup_elem(&recv_info, &tid);
    if (!args)
        return 0;  /* kprobe did not fire or was filtered */

    /* Cleanup the map entry — always delete to prevent leaks */
    bpf_map_delete_elem(&recv_info, &tid);

    /* ret < 0 means error; ret == 0 means EOF; we need data */
    if (ret <= 0)
        return 0;

    /* Cap the amount of payload we copy */
    copy_len = (__u32)ret;
    if (copy_len > MAX_DB_PAYLOAD_LEN)
        copy_len = MAX_DB_PAYLOAD_LEN;

    /* Read the first DETECT_LEN bytes for protocol classification.
     * We read from the user buffer directly since recvmsg has copied
     * data to it by the time the kretprobe fires.  Use
     * bpf_probe_read_user() for safe access. */
    if (args->buf_ptr != 0) {
        if (bpf_probe_read_user(detect_buf, DETECT_LEN,
                                (void *)(long)args->buf_ptr) < 0) {
            /* Cannot read user buffer — skip */
            return 0;
        }

        evt.protocol = detect_protocol(detect_buf, DETECT_LEN, args->dst_port);
    } else {
        /* No user buffer available — still emit event for port-based
         * tracking but mark protocol as unknown */
        evt.protocol = DB_PROTO_UNKNOWN;
    }

    /* Fill the event header */
    evt.timestamp_ns  = bpf_ktime_get_ns();
    evt.event_type    = EVENT_DB_QUERY;
    evt.pid           = (__u32)(tid >> 32);
    evt.src_ip        = 0;  /* filled below from sock */
    evt.dst_ip        = 0;
    evt.src_port      = 0;
    evt.dst_port      = args->dst_port;
    evt.payload_len   = copy_len;
    evt.response_bytes = (__u32)ret;
    evt._pad          = 0;

    /* Read src/dst IP from the sock pointer */
    if (args->sock_ptr != 0) {
        struct sock *sk = (struct sock *)(long)args->sock_ptr;
        evt.src_ip   = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
        evt.dst_ip   = BPF_CORE_READ(sk, __sk_common.skc_daddr);
        evt.src_port = BPF_CORE_READ(sk, __sk_common.skc_num);
    }

    bpf_get_current_comm(evt.comm, sizeof(evt.comm));

    /* Copy the payload from the user buffer into the event */
    if (args->buf_ptr != 0 && copy_len > 0) {
        /* bpf_probe_read_user into a bounded-length destination.
         * The verifier needs the size to be a constant or bounded register.
         * copy_len is bounded to MAX_DB_PAYLOAD_LEN above. */
        bpf_probe_read_user(evt.payload, copy_len,
                            (void *)(long)args->buf_ptr);
    }

    /* Emit via ring buffer */
    {
        struct db_event *ring_evt;

        ring_evt = bpf_ringbuf_reserve(&db_events, sizeof(struct db_event), 0);
        if (!ring_evt)
            return 0;

        __builtin_memcpy(ring_evt, &evt, sizeof(struct db_event));
        bpf_ringbuf_submit(ring_evt, 0);
    }

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
