"""Tests for BaseGate client initialization and exception mapping."""

import pytest

from basegate import BaseGate
from basegate._exceptions import (
    AuthenticationError,
    NotFoundError,
    PermissionError,
    RateLimitError,
    APIError,
    _raise_for_status,
)


def test_client_init():
    bg = BaseGate(api_key="sk-test", base_url="http://example.com", timeout=10.0)
    assert bg.responses is not None
    assert bg.sessions is not None
    assert bg.tools is not None
    bg.close()


def test_client_context_manager():
    with BaseGate(api_key="sk-test") as bg:
        assert bg.responses is not None


def test_raise_for_status_401():
    with pytest.raises(AuthenticationError):
        _raise_for_status(401, {"error": {"message": "invalid key", "code": "auth_error"}})


def test_raise_for_status_403():
    with pytest.raises(PermissionError):
        _raise_for_status(403, {"error": {"message": "denied", "code": "capability_denied"}})


def test_raise_for_status_404():
    with pytest.raises(NotFoundError):
        _raise_for_status(404, {"error": {"message": "not found"}})


def test_raise_for_status_429():
    with pytest.raises(RateLimitError):
        _raise_for_status(429, {"error": {"message": "rate limit"}})


def test_raise_for_status_500():
    with pytest.raises(APIError):
        _raise_for_status(500, {"error": {"message": "internal error"}})


def test_raise_for_status_200_no_raise():
    # Should not raise for success codes — but _raise_for_status is only called for >= 400
    # This tests that 400 generic raises BaseGateError
    from basegate._exceptions import BaseGateError
    with pytest.raises(BaseGateError):
        _raise_for_status(400, {"error": {"message": "bad request"}})
