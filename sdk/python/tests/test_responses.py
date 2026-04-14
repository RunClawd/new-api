"""Tests for Responses resource using respx mock."""

import httpx
import pytest
import respx

from basegate import BaseGate
from basegate._exceptions import NotFoundError, TimeoutError


BASE = "http://test.local"


@respx.mock
def test_create_sync():
    respx.post(f"{BASE}/v1/bg/responses").mock(return_value=httpx.Response(200, json={
        "id": "resp_1",
        "object": "response",
        "status": "succeeded",
        "model": "bg.llm.chat.standard",
        "output": [{"type": "text", "content": "Hello!", "role": "assistant"}],
        "usage": {"billable_units": 30, "billable_unit": "token", "input_units": 10, "output_units": 20},
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.responses.create(model="bg.llm.chat.standard", input={"messages": [{"role": "user", "content": "hi"}]})
    bg.close()

    assert resp.id == "resp_1"
    assert resp.status == "succeeded"
    assert len(resp.output) == 1
    assert resp.output[0].content == "Hello!"
    assert resp.usage.billable_units == 30


@respx.mock
def test_create_async():
    respx.post(f"{BASE}/v1/bg/responses").mock(return_value=httpx.Response(202, json={
        "id": "resp_async",
        "status": "queued",
        "model": "bg.video.generate.standard",
        "poll_url": "/v1/bg/responses/resp_async",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.responses.create(model="bg.video.generate.standard", input={"prompt": "test"}, mode="async")
    bg.close()

    assert resp.id == "resp_async"
    assert resp.status == "queued"
    assert resp.poll_url == "/v1/bg/responses/resp_async"


@respx.mock
def test_get():
    respx.get(f"{BASE}/v1/bg/responses/resp_42").mock(return_value=httpx.Response(200, json={
        "id": "resp_42",
        "status": "succeeded",
        "model": "bg.llm.chat.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.responses.get("resp_42")
    bg.close()

    assert resp.id == "resp_42"


@respx.mock
def test_cancel():
    respx.post(f"{BASE}/v1/bg/responses/resp_c/cancel").mock(return_value=httpx.Response(200, json={
        "id": "resp_c",
        "status": "canceled",
        "model": "bg.llm.chat.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.responses.cancel("resp_c")
    bg.close()

    assert resp.status == "canceled"


@respx.mock
def test_get_not_found():
    respx.get(f"{BASE}/v1/bg/responses/resp_nope").mock(return_value=httpx.Response(404, json={
        "error": {"code": "not_found", "message": "Response not found"},
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    with pytest.raises(NotFoundError):
        bg.responses.get("resp_nope")
    bg.close()


@respx.mock
def test_poll_success():
    # First call returns queued, second returns succeeded
    route = respx.get(f"{BASE}/v1/bg/responses/resp_poll")
    route.side_effect = [
        httpx.Response(200, json={"id": "resp_poll", "status": "queued", "model": "m"}),
        httpx.Response(200, json={"id": "resp_poll", "status": "succeeded", "model": "m"}),
    ]

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.responses.poll("resp_poll", interval=0.01, timeout=5.0)
    bg.close()

    assert resp.status == "succeeded"


@respx.mock
def test_poll_timeout():
    respx.get(f"{BASE}/v1/bg/responses/resp_stuck").mock(return_value=httpx.Response(200, json={
        "id": "resp_stuck", "status": "running", "model": "m",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    with pytest.raises(TimeoutError):
        bg.responses.poll("resp_stuck", interval=0.01, timeout=0.05)
    bg.close()


@respx.mock
def test_create_with_metadata():
    route = respx.post(f"{BASE}/v1/bg/responses").mock(return_value=httpx.Response(200, json={
        "id": "resp_meta", "status": "succeeded", "model": "bg.llm.chat.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    bg.responses.create(
        model="bg.llm.chat.standard",
        input={"messages": []},
        metadata={"user_id": "u123"},
    )
    bg.close()

    # Verify metadata was sent in body
    import json
    sent = json.loads(route.calls[0].request.content)
    assert sent["metadata"] == {"user_id": "u123"}
