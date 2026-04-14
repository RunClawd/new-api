"""Tools resource — list and execute tools."""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from .._client import HTTPClient
from .._types import Response, ToolDefinition


class Tools:
    """BaseGate Tools API."""

    def __init__(self, client: HTTPClient):
        self._client = client

    def list(self) -> List[ToolDefinition]:
        """List all available tools (capabilities with schemas).

        Returns:
            List of ToolDefinition objects compatible with OpenAI function calling.
        """
        data = self._client.get("/v1/bg/tools")
        items = data.get("data") or []
        return [ToolDefinition.from_dict(item) for item in items]

    def execute(
        self,
        name: str,
        arguments: Optional[Dict[str, Any]] = None,
        *,
        mode: str = "sync",
        metadata: Optional[Dict[str, str]] = None,
    ) -> Response:
        """Execute a tool by name.

        Converts the tool call into a BaseGate response dispatch.

        Args:
            name: Tool name (e.g. "bg_llm_chat_standard").
            arguments: Tool call arguments.
            mode: Execution mode (sync, async, stream).
            metadata: Optional metadata.
        """
        body: Dict[str, Any] = {"name": name}
        if arguments:
            body["arguments"] = arguments
        if mode != "sync":
            body["mode"] = mode
        if metadata:
            body["metadata"] = metadata

        data = self._client.post("/v1/bg/tools/execute", json=body)
        return Response.from_dict(data)
