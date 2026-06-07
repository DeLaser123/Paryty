---
description: "C/C++ coding standards. Use when writing C/C++ or eBPF code."
applyTo: "**/*.{c,h,cpp}"
---

# C Coding Bible — Safety-Critical eBPF Programs

Sources: NASA JPL "Power of 10" (Holzmann), SEI CERT C Coding Standard (CMU), MISRA C:2023, Linux Kernel Coding Style (Torvalds), "The C Programming Language" (Kernighan/Ritchie), BPF verifier constraints.

## I. NASA Power of 10 (Safety-Critical, Non-Negotiable)

These rules from NASA JPL's Laboratory for Reliable Software are mandatory for all Paryty C code (eBPF programs):

1. **No `goto`, `setjmp`, `longjmp`, or recursion.** Simple control flow only.
2. **All loops have a fixed upper bound.** `for (i = 0; i < MAX_ENTRIES; i++)` — the BPF verifier requires this.
3. **No dynamic memory allocation after initialization.** No `malloc`, `calloc`, `free`. BPF programs cannot allocate on the heap — all data on stack or in BPF maps.
4. **No function longer than 60 lines** (one printed page). Each BPF program does one thing. Note: exceeds the cross-language 50-line limit; this is an intentional exception from NASA JPL Power of 10 — BPF programs trade conciseness for verifier safety.
5. **Minimum 2 assertions per function.** Validate inputs, validate map lookups, validate bounds.
6. **Declare all data objects at smallest possible scope.** Minimize variable visibility.
7. **Check all return values of non-void functions.** Check all parameters of called functions.
8. **Preprocessor limited to `#include` and simple `#define`.** No token pasting, no variadic macros, no conditional compilation beyond feature flags.
9. **Single pointer dereference only.** No `ptr->field->subfield`. Extract to local variable first.
10. **Compile with ALL warnings enabled. Zero warnings tolerated.** `-Wall -Wextra -Werror` for userspace. BPF verifier for kernel programs.

## II. SEI CERT C — Memory Safety

### Buffer Overflows (CERT ARR38-C)
11. **Never use `strcpy`, `strcat`, `sprintf`, `gets`.** Use `strncpy`, `strncat`, `snprintf`, `fgets`.
12. **Always null-terminate strings.** After `strncpy`, explicitly set `dst[size-1] = '\0'`.
13. **Bounds-check all array accesses.** `if (index >= 0 && index < ARRAY_SIZE(arr))`.
14. **Use `sizeof` for buffer sizes.** Never hardcode buffer sizes — they drift from declarations.

### Integer Safety (CERT INT30-C, INT32-C)
15. **Check for integer overflow before arithmetic.** `if (a > INT_MAX - b)` before `a + b`.
16. **Use fixed-width types:** `uint32_t`, `int64_t`, `uint8_t` — from `<stdint.h>`.
17. **Never compare signed and unsigned.** Cast explicitly or use matching types.
18. **Validate all division operands.** Check for zero before division.

### Pointer Safety (CERT EXP34-C)
19. **Never dereference NULL.** Always check pointer return from map lookups: `if (val != NULL)`.
20. **Never use dangling pointers.** Set to NULL after free (in userspace). In BPF, pointers are stack-only.
21. **Never perform pointer arithmetic without bounds checking.**
22. **Use `const` for read-only parameters.** `const struct event *evt` — documents intent, enables optimization.

## III. BPF Verifier Constraints (Kernel-Specific)

23. **No unbounded loops.** Use `#pragma unroll` for small known counts, or `for (i = 0; i < MAX; i++)` for verifier-visible bounds.
24. **No heap allocation.** Stack (512 bytes max per program) or BPF maps only.
25. **Stack usage must stay under 512 bytes.** Use `__attribute__((always_inline))` judiciously — inlining increases stack usage.
26. **Use `bpf_probe_read_kernel()` for all kernel memory reads.** Never dereference kernel pointers directly.
27. **BPF maps must have bounded `max_entries`.** Prevent kernel memory exhaustion.
28. **Use `BPF_MAP_TYPE_RINGBUF` over `BPF_MAP_TYPE_PERF_EVENT_ARRAY`** — better performance, single mmap region.
29. **Use `BPF_MAP_TYPE_HASH` for connection state** — O(1) lookup, bounded size.
30. **Program size: <4096 instructions** (after inlining). Split complex logic across programs.
31. **Use `CO-RE` (Compile Once, Run Everywhere)** — `vmlinux.h` from BTF, no hardcoded offsets.

## IV. MISRA C — Critical Rules Subset

32. **No implicit type conversions.** Cast explicitly: `(uint32_t)value`.
33. **No comma operator.** One statement per line.
34. **All `switch` statements have a `default` case.** Even if it's `break;`.
35. **No fall-through in `switch` cases.** Every case ends with `break` or `return`.
36. **No bitwise operations on signed integers.** Use unsigned types for bit manipulation.
37. **Boolean expressions use explicit comparison.** `if (ptr != NULL)` not `if (ptr)`.

## V. Linux Kernel Style (Torvalds)

38. **Tabs for indentation (8-space tabs).** Kernel style.
39. **Braces on same line for control structures:** `if (x) {`.
40. **Braces on next line for function definitions:**
    ```c
    static int my_function(void)
    {
    ```
41. **Descriptive variable names in large scope, short names in small scope.**
42. **Comment WHAT the code does, not HOW.** The code shows how.
43. **Keep functions small and focused.** Each BPF kprobe handler does one thing.

## VI. eBPF-Specific Patterns

### Event Structure Design
44. **Fixed-size event structs.** No variable-length fields. Use fixed buffers: `char name[64]`.
45. **Pack structs for ring buffer:** `struct event { ... } __attribute__((packed));`.
46. **Include timestamp in every event:** `uint64_t timestamp_ns = bpf_ktime_get_ns();`.
47. **Include PID/TGID for attribution:** `uint64_t pid_tgid = bpf_get_current_pid_tgid();`.

### Map Design
48. **Separate maps for separate concerns.** Don't overload one map with different data types.
49. **Size maps appropriately.** `max_entries` based on expected concurrent connections, not arbitrary large numbers.
50. **Clean up map entries.** Delete connection state on `tcp_close` to prevent map exhaustion.

### Error Handling in BPF
51. **BPF programs never return errors to userspace.** They return 0 (continue) or drop the packet.
52. **Log errors via ring buffer events.** Create an `error_event` type for diagnostic purposes.
53. **Graceful degradation.** If a map lookup fails, skip the event — don't crash the kernel.

## VII. Userspace C Code (Loader/Helpers)

54. **Use `static` for all file-scope functions and variables.** Minimize exported symbols.
55. **Initialize all variables at declaration.** `int fd = -1;` not `int fd;`.
56. **Check all system call returns.** `if (fd < 0) { perror("open"); return -1; }`.
57. **Use `RAII`-style cleanup.** `goto cleanup;` pattern for resource management in userspace.
58. **`const` correctness everywhere.** If a function doesn't modify a parameter, declare it `const`.

## VIII. Forbidden Patterns

59. **No `malloc`/`free` in BPF programs.** Stack and maps only.
60. **No `goto` in BPF programs.** Simple control flow.
61. **No unbounded loops.** Verifier will reject.
62. **No recursive functions.** Verifier will reject.
63. **No raw kernel pointer dereference.** Use `bpf_probe_read_kernel`.
64. **No hardcoded kernel struct offsets.** Use CO-RE + vmlinux.h.
65. **No `transmute`-style casts between unrelated pointer types.**
66. **No global variables in BPF programs.** Use maps.

