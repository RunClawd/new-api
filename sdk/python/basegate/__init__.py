"""BaseGate Python SDK — Unified AI Capability Gateway."""

from __future__ import annotations

from typing import Optional

from ._client import HTTPClient
from ._exceptions import (
    APIError,
    AuthenticationError,
    BaseGateError,
    NotFoundError,
    PermissionError,
    RateLimitError,
    TimeoutError,
)
from ._types import (
    FunctionSchema,
    OutputItem,
    Response,
    Session,
    SessionAction,
    StreamEvent,
    ToolDefinition,
    Usage,
)
from ._version import __version__
from .resources import Responses, Sessions, Tools


class BaseGate:
    """BaseGate API client.

    Example::

        from basegate import BaseGate

        bg = BaseGate(api_key="sk-xxx")

        # Sync
        result = bg.responses.create(model="bg.llm.chat.standard", input={"messages": [...]})

        # Stream
        for event in bg.responses.stream(model="bg.llm.chat.standard", input={"messages": [...]}):
            if event.delta:
                print(event.delta, end="")

        # Tools
        tools = bg.tools.list()
    """

    def __init__(
        self,
        api_key: str,
        *,
        base_url: str = "http://localhost:3000",
        timeout: float = 30.0,
        max_retries: int = 2,
        project_id: Optional[str] = None,
    ):
        self._http = HTTPClient(
            api_key=api_key,
            base_url=base_url,
            timeout=timeout,
            max_retries=max_retries,
            project_id=project_id,
        )
        self.responses = Responses(self._http)
        self.sessions = Sessions(self._http)
        self.tools = Tools(self._http)

    def close(self) -> None:
        """Close the underlying HTTP client."""
        self._http.close()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()


__all__ = [
    "BaseGate",
    "__version__",
    # Types
    "Response",
    "OutputItem",
    "Usage",
    "StreamEvent",
    "ToolDefinition",
    "FunctionSchema",
    "Session",
    "SessionAction",
    # Exceptions
    "BaseGateError",
    "AuthenticationError",
    "PermissionError",
    "NotFoundError",
    "RateLimitError",
    "APIError",
    "TimeoutError",
]
