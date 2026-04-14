"""Tests for type deserialization."""

from basegate._types import Response, ToolDefinition, Session, SessionAction


def test_response_from_dict_minimal():
    data = {"id": "resp_1", "status": "succeeded", "model": "bg.llm.chat.standard"}
    r = Response.from_dict(data)
    assert r.id == "resp_1"
    assert r.status == "succeeded"
    assert r.model == "bg.llm.chat.standard"
    assert r.output == []
    assert r.usage is None
    assert r.error is None


def test_response_from_dict_with_output():
    data = {
        "id": "resp_2",
        "status": "succeeded",
        "model": "bg.llm.chat.standard",
        "output": [
            {"type": "text", "content": "Hello!", "role": "assistant"},
        ],
        "usage": {
            "billable_units": 150,
            "billable_unit": "token",
            "input_units": 50,
            "output_units": 100,
        },
    }
    r = Response.from_dict(data)
    assert len(r.output) == 1
    assert r.output[0].type == "text"
    assert r.output[0].content == "Hello!"
    assert r.output[0].role == "assistant"
    assert r.usage.billable_units == 150
    assert r.usage.input_units == 50


def test_response_from_dict_with_error():
    data = {
        "id": "resp_err",
        "status": "failed",
        "model": "bg.llm.chat.standard",
        "error": {"code": "provider_error", "message": "upstream 500"},
    }
    r = Response.from_dict(data)
    assert r.error is not None
    assert r.error.code == "provider_error"
    assert r.error.message == "upstream 500"


def test_tool_definition_from_dict():
    data = {
        "type": "function",
        "function": {
            "name": "bg_llm_chat_standard",
            "description": "Standard LLM chat",
            "parameters": {"type": "object", "properties": {"messages": {"type": "array"}}},
        },
    }
    t = ToolDefinition.from_dict(data)
    assert t.type == "function"
    assert t.function.name == "bg_llm_chat_standard"
    assert "messages" in t.function.parameters["properties"]


def test_session_from_dict():
    data = {"id": "sess_1", "status": "active", "model": "bg.sandbox.session.standard", "response_id": "resp_x"}
    s = Session.from_dict(data)
    assert s.id == "sess_1"
    assert s.status == "active"
    assert s.response_id == "resp_x"


def test_session_action_from_dict():
    data = {"id": "act_1", "session_id": "sess_1", "status": "succeeded", "output": {"stdout": "42\n"}}
    a = SessionAction.from_dict(data)
    assert a.id == "act_1"
    assert a.output == {"stdout": "42\n"}
