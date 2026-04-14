"""BaseGate SDK type definitions."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


@dataclass
class OutputItem:
    type: str  # text | image | video | audio | file | session | tool_call
    content: Any
    role: Optional[str] = None


@dataclass
class Usage:
    billable_units: float = 0
    billable_unit: str = ""
    input_units: float = 0
    output_units: float = 0


@dataclass
class Pricing:
    billing_mode: str = ""
    billable_unit: str = ""
    unit_price: float = 0
    total: float = 0
    currency: str = "USD"


@dataclass
class Error:
    code: str = ""
    message: str = ""
    type: str = ""
    detail: str = ""


@dataclass
class Response:
    id: str = ""
    object: str = "response"
    created_at: int = 0
    status: str = ""
    model: str = ""
    output: List[OutputItem] = field(default_factory=list)
    usage: Optional[Usage] = None
    pricing: Optional[Pricing] = None
    error: Optional[Error] = None
    poll_url: Optional[str] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> Response:
        output = []
        for item in data.get("output") or []:
            output.append(OutputItem(
                type=item.get("type", ""),
                content=item.get("content"),
                role=item.get("role"),
            ))
        usage = None
        if data.get("usage"):
            u = data["usage"]
            usage = Usage(
                billable_units=u.get("billable_units", 0),
                billable_unit=u.get("billable_unit", ""),
                input_units=u.get("input_units", 0),
                output_units=u.get("output_units", 0),
            )
        error = None
        if data.get("error"):
            e = data["error"]
            error = Error(
                code=e.get("code", ""),
                message=e.get("message", ""),
                type=e.get("type", ""),
                detail=e.get("detail", ""),
            )
        return cls(
            id=data.get("id", ""),
            object=data.get("object", "response"),
            created_at=data.get("created_at", 0),
            status=data.get("status", ""),
            model=data.get("model", ""),
            output=output,
            usage=usage,
            error=error,
            poll_url=data.get("poll_url"),
        )


@dataclass
class StreamEvent:
    event: str  # e.g. response.output_text.delta, response.completed
    data: Dict[str, Any]
    delta: Optional[str] = None


@dataclass
class FunctionSchema:
    name: str = ""
    description: str = ""
    parameters: Optional[Dict[str, Any]] = None


@dataclass
class ToolDefinition:
    type: str = "function"
    function: Optional[FunctionSchema] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> ToolDefinition:
        fn_data = data.get("function", {})
        fn = FunctionSchema(
            name=fn_data.get("name", ""),
            description=fn_data.get("description", ""),
            parameters=fn_data.get("parameters"),
        )
        return cls(type=data.get("type", "function"), function=fn)


@dataclass
class Session:
    id: str = ""
    object: str = "session"
    created_at: int = 0
    status: str = ""
    model: str = ""
    response_id: str = ""

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> Session:
        return cls(
            id=data.get("id", ""),
            object=data.get("object", "session"),
            created_at=data.get("created_at", 0),
            status=data.get("status", ""),
            model=data.get("model", ""),
            response_id=data.get("response_id", ""),
        )


@dataclass
class SessionAction:
    id: str = ""
    session_id: str = ""
    status: str = ""
    output: Any = None
    error: Optional[Error] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> SessionAction:
        error = None
        if data.get("error"):
            e = data["error"]
            error = Error(code=e.get("code", ""), message=e.get("message", ""))
        return cls(
            id=data.get("id", ""),
            session_id=data.get("session_id", ""),
            status=data.get("status", ""),
            output=data.get("output"),
            error=error,
        )
