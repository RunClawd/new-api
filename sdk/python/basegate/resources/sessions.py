"""Sessions resource — create, execute, close, get."""

from __future__ import annotations

from typing import Any, Dict, Optional

from .._client import HTTPClient
from .._types import Session, SessionAction


class Sessions:
    """BaseGate Sessions API."""

    def __init__(self, client: HTTPClient):
        self._client = client

    def create(
        self,
        model: str,
        *,
        input: Any = None,
        metadata: Optional[Dict[str, str]] = None,
    ) -> Session:
        """Create a new session.

        Args:
            model: Capability name (e.g. "bg.sandbox.session.standard").
            input: Optional initial input.
            metadata: Optional metadata.
        """
        body: Dict[str, Any] = {"model": model}
        if input is not None:
            body["input"] = input
        if metadata:
            body["metadata"] = metadata

        data = self._client.post("/v1/bg/sessions", json=body)
        return Session.from_dict(data)

    def get(self, session_id: str) -> Session:
        """Get session state by ID."""
        data = self._client.get(f"/v1/bg/sessions/{session_id}")
        return Session.from_dict(data)

    def execute(
        self,
        session_id: str,
        action: str,
        *,
        input: Any = None,
        idempotency_key: Optional[str] = None,
    ) -> SessionAction:
        """Execute an action against a session.

        Args:
            session_id: The session ID.
            action: Action type (e.g. "execute", "upload").
            input: Action input (e.g. {"code": "print(42)"}).
            idempotency_key: Optional idempotency key for retry safety.
        """
        body: Dict[str, Any] = {"action": action}
        if input is not None:
            body["input"] = input
        if idempotency_key:
            body["idempotency_key"] = idempotency_key

        data = self._client.post(f"/v1/bg/sessions/{session_id}/action", json=body)
        return SessionAction.from_dict(data)

    def close(self, session_id: str) -> Session:
        """Close a session."""
        data = self._client.post(f"/v1/bg/sessions/{session_id}/close")
        return Session.from_dict(data)
