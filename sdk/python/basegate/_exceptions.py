"""BaseGate SDK exception hierarchy."""

from __future__ import annotations


class BaseGateError(Exception):
    """Base exception for all BaseGate SDK errors."""

    def __init__(self, message: str, status_code: int | None = None, code: str | None = None):
        super().__init__(message)
        self.status_code = status_code
        self.code = code


class AuthenticationError(BaseGateError):
    """API key is missing, invalid, or expired (401)."""


class PermissionError(BaseGateError):
    """Capability denied by policy (403)."""


class NotFoundError(BaseGateError):
    """Resource not found (404)."""


class RateLimitError(BaseGateError):
    """Rate limit exceeded (429)."""


class APIError(BaseGateError):
    """Server-side error (5xx)."""


class TimeoutError(BaseGateError):
    """Request or poll timeout."""


def _raise_for_status(status_code: int, body: dict) -> None:
    """Raise appropriate exception based on HTTP status code and error body."""
    error = body.get("error", {})
    message = error.get("message", "") or str(body)
    code = error.get("code", "")

    if status_code == 401:
        raise AuthenticationError(message, status_code, code)
    elif status_code == 403:
        raise PermissionError(message, status_code, code)
    elif status_code == 404:
        raise NotFoundError(message, status_code, code)
    elif status_code == 429:
        raise RateLimitError(message, status_code, code)
    elif status_code >= 500:
        raise APIError(message, status_code, code)
    elif status_code >= 400:
        raise BaseGateError(message, status_code, code)
