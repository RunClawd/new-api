"""SSE (Server-Sent Events) stream parser for httpx responses."""

from __future__ import annotations

import json
from typing import Any, Dict, Generator, Tuple

from ._types import StreamEvent


def iter_sse_events(response) -> Generator[StreamEvent, None, None]:
    """Parse SSE stream from an httpx streaming response.

    Yields StreamEvent objects for each data-bearing SSE block.
    Stops when it encounters ``data: [DONE]``.
    """
    buffer = ""
    for chunk in response.iter_text():
        buffer += chunk
        while "\n\n" in buffer:
            block, buffer = buffer.split("\n\n", 1)
            if not block.strip():
                continue
            event_type = ""
            data_str = ""
            for line in block.split("\n"):
                if line.startswith("event: "):
                    event_type = line[7:].strip()
                elif line.startswith("data: "):
                    data_str = line[6:].strip()
            if not data_str:
                continue
            if data_str == "[DONE]":
                return

            data: Dict[str, Any] = {}
            try:
                data = json.loads(data_str)
            except json.JSONDecodeError:
                data = {"raw": data_str}

            delta = data.get("delta") if event_type == "response.output_text.delta" else None

            yield StreamEvent(event=event_type, data=data, delta=delta)
