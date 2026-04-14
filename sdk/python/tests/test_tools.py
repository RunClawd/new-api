"""Tests for Tools resource using respx mock."""

import httpx
import respx

from basegate import BaseGate


BASE = "http://test.local"


@respx.mock
def test_list_tools():
    respx.get(f"{BASE}/v1/bg/tools").mock(return_value=httpx.Response(200, json={
        "object": "list",
        "data": [
            {
                "type": "function",
                "function": {
                    "name": "bg_llm_chat_standard",
                    "description": "Standard LLM chat [modes: sync,stream, billing: token]",
                    "parameters": {"type": "object", "properties": {"messages": {"type": "array"}}},
                },
            },
            {
                "type": "function",
                "function": {
                    "name": "bg_video_generate_standard",
                    "description": "Video generation [modes: async, billing: second]",
                    "parameters": {"type": "object", "properties": {"prompt": {"type": "string"}}},
                },
            },
        ],
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    tools = bg.tools.list()
    bg.close()

    assert len(tools) == 2
    assert tools[0].type == "function"
    assert tools[0].function.name == "bg_llm_chat_standard"
    assert "messages" in tools[0].function.parameters["properties"]
    assert tools[1].function.name == "bg_video_generate_standard"


@respx.mock
def test_list_tools_empty():
    respx.get(f"{BASE}/v1/bg/tools").mock(return_value=httpx.Response(200, json={
        "object": "list",
        "data": [],
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    tools = bg.tools.list()
    bg.close()

    assert tools == []


@respx.mock
def test_execute_tool_sync():
    respx.post(f"{BASE}/v1/bg/tools/execute").mock(return_value=httpx.Response(200, json={
        "id": "resp_tool_1",
        "status": "succeeded",
        "model": "bg.llm.chat.standard",
        "output": [{"type": "text", "content": "Tool result!"}],
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.tools.execute(
        name="bg_llm_chat_standard",
        arguments={"messages": [{"role": "user", "content": "test"}]},
    )
    bg.close()

    assert resp.id == "resp_tool_1"
    assert resp.status == "succeeded"
    assert resp.output[0].content == "Tool result!"


@respx.mock
def test_execute_tool_async():
    route = respx.post(f"{BASE}/v1/bg/tools/execute").mock(return_value=httpx.Response(202, json={
        "id": "resp_tool_async",
        "status": "queued",
        "model": "bg.video.generate.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    resp = bg.tools.execute(
        name="bg_video_generate_standard",
        arguments={"prompt": "sunset"},
        mode="async",
    )
    bg.close()

    assert resp.status == "queued"

    import json
    sent = json.loads(route.calls[0].request.content)
    assert sent["mode"] == "async"


@respx.mock
def test_execute_tool_with_metadata():
    route = respx.post(f"{BASE}/v1/bg/tools/execute").mock(return_value=httpx.Response(200, json={
        "id": "resp_tool_m", "status": "succeeded", "model": "bg.llm.chat.standard",
    }))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0)
    bg.tools.execute(
        name="bg_llm_chat_standard",
        arguments={"messages": []},
        metadata={"trace_id": "t1"},
    )
    bg.close()

    import json
    sent = json.loads(route.calls[0].request.content)
    assert sent["metadata"] == {"trace_id": "t1"}


@respx.mock
def test_project_id_header():
    route = respx.get(f"{BASE}/v1/bg/tools").mock(return_value=httpx.Response(200, json={"object": "list", "data": []}))

    bg = BaseGate(api_key="sk-test", base_url=BASE, max_retries=0, project_id="proj_abc")
    bg.tools.list()
    bg.close()

    assert route.calls[0].request.headers["X-Project-Id"] == "proj_abc"
