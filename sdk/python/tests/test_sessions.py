"""Tests for Sessions resource using respx mock."""

import httpx
import respx

from basegate import BaseGate


BASE = "http://test.local"


@respx.mock
def test_create_session():
    respx.post(f"{BASE}/v1/bg/sessions").mock(return_value=httpx.Response(200, json={
        "id": "sess_1",
        "object": "session",
        "status": "active",
        "model": "bg.sandbox.session.standard",
        "response_id": "resp_s1",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    sess = bg.sessions.create(model="bg.sandbox.session.standard")
    bg.close()

    assert sess.id == "sess_1"
    assert sess.status == "active"
    assert sess.response_id == "resp_s1"


@respx.mock
def test_get_session():
    respx.get(f"{BASE}/v1/bg/sessions/sess_2").mock(return_value=httpx.Response(200, json={
        "id": "sess_2",
        "status": "idle",
        "model": "bg.sandbox.session.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    sess = bg.sessions.get("sess_2")
    bg.close()

    assert sess.id == "sess_2"
    assert sess.status == "idle"


@respx.mock
def test_execute_action():
    respx.post(f"{BASE}/v1/bg/sessions/sess_3/action").mock(return_value=httpx.Response(200, json={
        "id": "act_1",
        "session_id": "sess_3",
        "status": "succeeded",
        "output": {"stdout": "42\n", "stderr": "", "exit_code": 0},
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    action = bg.sessions.execute("sess_3", action="execute", input={"code": "print(42)"})
    bg.close()

    assert action.id == "act_1"
    assert action.status == "succeeded"
    assert action.output["stdout"] == "42\n"


@respx.mock
def test_execute_with_idempotency():
    route = respx.post(f"{BASE}/v1/bg/sessions/sess_4/action").mock(return_value=httpx.Response(200, json={
        "id": "act_2", "session_id": "sess_4", "status": "succeeded",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    bg.sessions.execute("sess_4", action="execute", input={"code": "x"}, idempotency_key="idem_1")
    bg.close()

    import json
    sent = json.loads(route.calls[0].request.content)
    assert sent["idempotency_key"] == "idem_1"


@respx.mock
def test_close_session():
    respx.post(f"{BASE}/v1/bg/sessions/sess_5/close").mock(return_value=httpx.Response(200, json={
        "id": "sess_5",
        "status": "closed",
        "model": "bg.sandbox.session.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    sess = bg.sessions.close("sess_5")
    bg.close()

    assert sess.status == "closed"
