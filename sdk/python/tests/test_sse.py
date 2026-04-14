"""Tests for SSE stream parser."""

from basegate._sse import iter_sse_events


class FakeStreamResponse:
    """Mock httpx streaming response."""

    def __init__(self, text: str):
        self._text = text

    def iter_text(self):
        yield self._text


def test_parse_single_event():
    raw = 'event: response.output_text.delta\ndata: {"delta":"Hello"}\n\n'
    resp = FakeStreamResponse(raw)
    events = list(iter_sse_events(resp))
    assert len(events) == 1
    assert events[0].event == "response.output_text.delta"
    assert events[0].delta == "Hello"
    assert events[0].data["delta"] == "Hello"


def test_parse_multiple_events():
    raw = (
        'event: response.output_text.delta\ndata: {"delta":"Hi"}\n\n'
        'event: response.output_text.delta\ndata: {"delta":" world"}\n\n'
        'event: response.completed\ndata: {"status":"succeeded"}\n\n'
    )
    resp = FakeStreamResponse(raw)
    events = list(iter_sse_events(resp))
    assert len(events) == 3
    assert events[0].delta == "Hi"
    assert events[1].delta == " world"
    assert events[2].event == "response.completed"
    assert events[2].delta is None


def test_parse_done_signal():
    raw = (
        'event: response.output_text.delta\ndata: {"delta":"x"}\n\n'
        'data: [DONE]\n\n'
        'event: should_not_appear\ndata: {"extra":"ignored"}\n\n'
    )
    resp = FakeStreamResponse(raw)
    events = list(iter_sse_events(resp))
    assert len(events) == 1
    assert events[0].delta == "x"


def test_parse_empty_blocks_ignored():
    raw = '\n\nevent: test\ndata: {"ok":true}\n\n\n\n'
    resp = FakeStreamResponse(raw)
    events = list(iter_sse_events(resp))
    assert len(events) == 1
    assert events[0].event == "test"


def test_parse_non_json_data():
    raw = 'event: raw\ndata: plain text here\n\n'
    resp = FakeStreamResponse(raw)
    events = list(iter_sse_events(resp))
    assert len(events) == 1
    assert events[0].data == {"raw": "plain text here"}
