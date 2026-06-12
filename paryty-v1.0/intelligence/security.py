"""
Filesystem-safety helpers shared across the intelligence layer.

Model artifact filenames are derived from RPC-supplied strings
(``metric_name`` comes straight from the network). Without sanitization a
crafted name like ``../../etc/cron.d/x`` becomes a path-traversal write —
and because model artifacts are deserialized with joblib (pickle), a
traversal-written file is one step from remote code execution.
"""

from __future__ import annotations

import re

# Allow only word characters, dots and dashes; everything else collapses to _.
_SAFE_COMPONENT = re.compile(r"[^A-Za-z0-9._-]+")

# Maximum length for a single filename component (filesystem limits minus
# room for prefixes/suffixes like "_v12.joblib").
_MAX_COMPONENT_LEN = 100


def sanitize_filename_component(raw: str) -> str:
    """Return a filesystem-safe version of an untrusted name component.

    Guarantees:
        * never empty (falls back to ``"unnamed"``)
        * no path separators or parent references (``..`` collapses)
        * bounded length
    """
    cleaned = _SAFE_COMPONENT.sub("_", raw).strip("._")
    # Collapse any remaining ".." sequences defensively.
    while ".." in cleaned:
        cleaned = cleaned.replace("..", "_")
    if not cleaned:
        cleaned = "unnamed"
    return cleaned[:_MAX_COMPONENT_LEN]
