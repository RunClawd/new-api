# BaseGate Python SDK

Python client for the BaseGate Unified AI Capability Gateway.

## Installation

```bash
pip install ./sdk/python
```

## Quick Start

```python
from basegate import BaseGate

bg = BaseGate(api_key="sk-xxx", base_url="http://localhost:3000")

# Sync request
result = bg.responses.create(
    model="bg.llm.chat.standard",
    input={"messages": [{"role": "user", "content": "Hello!"}]},
)
print(result.output[0].content)

# Streaming
for event in bg.responses.stream(
    model="bg.llm.chat.standard",
    input={"messages": [{"role": "user", "content": "Hello!"}]},
):
    if event.delta:
        print(event.delta, end="", flush=True)

# Async with polling
resp = bg.responses.create(
    model="bg.video.generate.standard",
    input={"prompt": "A sunset over mountains"},
    mode="async",
)
result = bg.responses.poll(resp.id, interval=2.0, timeout=120.0)

# Sessions
session = bg.sessions.create(model="bg.sandbox.session.standard")
action = bg.sessions.execute(session.id, action="execute", input={"code": "print(42)"})
bg.sessions.close(session.id)

# Tools (OpenAI function calling compatible)
tools = bg.tools.list()
result = bg.tools.execute(name="bg_llm_chat_standard", arguments={"messages": [...]})
```

## Features

- Sync, async, and streaming execution modes
- Automatic retry with exponential backoff (429, 5xx)
- SSE stream parsing for real-time output
- Session lifecycle management
- Tool projection compatible with OpenAI function calling
- Type hints on all public APIs
