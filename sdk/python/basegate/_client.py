"""BaseGate HTTP client wrapper around httpx."""

from __future__ import annotations

import time
from typing import Any, Dict, Generator, Optional

import httpx

from ._exceptions import TimeoutError, _raise_for_status
from ._sse import iter_sse_events
from ._types import StreamEvent
from ._version import __version__


_RETRYABLE_STATUS = {429, 500, 502, 503}
_DEFAULT_BASE_URL = "http://localhost:3000"


class HTTPClient:
    """Low-level HTTP client with auth, retry, and streaming support."""

    def __init__(
        self,
        api_key: str,
        base_url: str = _DEFAULT_BASE_URL,
        timeout: float = 30.0,
        max_retries: int = 2,
        project_id: Optional[str] = None,
    ):
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.max_retries = max_retries
        self.project_id = project_id

        headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "User-Agent": f"basegate-python/{__version__}",
        }
        if project_id:
            headers["X-Project-Id"] = project_id

        self._client = httpx.Client(
            base_url=self.base_url,
            headers=headers,
            timeout=httpx.Timeout(timeout),
        )

    def close(self) -> None:
        self._client.close()

    def request(self, method: str, path: str, **kwargs) -> Dict[str, Any]:
        """Make an HTTP request with automatic retry on transient errors."""
        last_exc: Optional[Exception] = None
        for attempt in range(1 + self.max_retries):
            try:
                resp = self._client.request(method, path, **kwargs)
                if resp.status_code in _RETRYABLE_STATUS and attempt < self.max_retries:
                    _wait_backoff(attempt)
                    continue
                if resp.status_code >= 400:
                    body = resp.json() if resp.headers.get("content-type", "").startswith("application/json") else {}
                    _raise_for_status(resp.status_code, body)
                return resp.json()
            except httpx.TimeoutException as e:
                last_exc = e
                if attempt < self.max_retries:
                    _wait_backoff(attempt)
                    continue
            except httpx.HTTPError as e:
                last_exc = e
                if attempt < self.max_retries:
                    _wait_backoff(attempt)
                    continue
        raise TimeoutError(f"Request failed after {self.max_retries + 1} attempts: {last_exc}")

    def get(self, path: str, **kwargs) -> Dict[str, Any]:
        return self.request("GET", path, **kwargs)

    def post(self, path: str, json: Any = None, **kwargs) -> Dict[str, Any]:
        return self.request("POST", path, json=json, **kwargs)

    def stream_post(self, path: str, json: Any = None) -> Generator[StreamEvent, None, None]:
        """Make a streaming POST request and yield SSE events."""
        with self._client.stream("POST", path, json=json) as resp:
            if resp.status_code >= 400:
                body = {}
                try:
                    text = resp.read().decode()
                    import json as _json
                    body = _json.loads(text)
                except Exception:
                    pass
                _raise_for_status(resp.status_code, body)
            yield from iter_sse_events(resp)


def _wait_backoff(attempt: int) -> None:
    """Exponential backoff: 0.5s, 1s, 2s, ..."""
    time.sleep(min(0.5 * (2 ** attempt), 8.0))
