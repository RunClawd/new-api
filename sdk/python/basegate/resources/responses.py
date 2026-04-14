"""Responses resource — create, get, cancel, stream, poll."""

from __future__ import annotations

import time
from typing import Any, Dict, Generator, Optional

from .._client import HTTPClient
from .._exceptions import TimeoutError
from .._types import Response, StreamEvent


class Responses:
    """BaseGate Responses API."""

    def __init__(self, client: HTTPClient):
        self._client = client

    def create(
        self,
        model: str,
        input: Any,
        *,
        mode: str = "sync",
        metadata: Optional[Dict[str, str]] = None,
    ) -> Response:
        """Create a response (sync or async).

        Args:
            model: Capability name (e.g. "bg.llm.chat.standard").
            input: Request input (dict or string).
            mode: Execution mode — "sync", "async", or "stream" (for stream use .stream()).
            metadata: Optional key-value metadata.
        """
        body: Dict[str, Any] = {"model": model, "input": input}
        if mode != "sync":
            body["execution_options"] = {"mode": mode}
        if metadata:
            body["metadata"] = metadata

        data = self._client.post("/v1/bg/responses", json=body)
        return Response.from_dict(data)

    def get(self, response_id: str) -> Response:
        """Get a response by ID."""
        data = self._client.get(f"/v1/bg/responses/{response_id}")
        return Response.from_dict(data)

    def cancel(self, response_id: str) -> Response:
        """Cancel an in-progress response."""
        data = self._client.post(f"/v1/bg/responses/{response_id}/cancel")
        return Response.from_dict(data)

    def stream(
        self,
        model: str,
        input: Any,
        *,
        metadata: Optional[Dict[str, str]] = None,
    ) -> Generator[StreamEvent, None, None]:
        """Create a streaming response and yield SSE events.

        Args:
            model: Capability name.
            input: Request input.
            metadata: Optional metadata.

        Yields:
            StreamEvent objects. Use event.delta for text deltas.
        """
        body: Dict[str, Any] = {
            "model": model,
            "input": input,
            "execution_options": {"mode": "stream"},
        }
        if metadata:
            body["metadata"] = metadata

        yield from self._client.stream_post("/v1/bg/responses", json=body)

    def poll(
        self,
        response_id: str,
        *,
        interval: float = 2.0,
        timeout: float = 120.0,
    ) -> Response:
        """Poll an async response until it reaches a terminal state.

        Args:
            response_id: The response ID to poll.
            interval: Seconds between polls.
            timeout: Maximum total wait time in seconds.

        Returns:
            The final Response object.

        Raises:
            TimeoutError: If timeout is exceeded.
        """
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            resp = self.get(response_id)
            if resp.status in ("succeeded", "failed", "canceled"):
                return resp
            time.sleep(interval)
        raise TimeoutError(f"Polling timed out after {timeout}s for response {response_id}")
