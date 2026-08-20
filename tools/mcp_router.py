#!/usr/bin/env python3
"""Tiny local MCP-style adapter for the Morph router, with no dependencies."""

import argparse
import json
import os
import sys
import urllib.request

ROUTER_URL = os.getenv("ROUTER_URL", "http://localhost:8000")


def route_coding_task(context_len: int, task_type: str, diff_size: int) -> dict:
    """Choose the simulated model backend for a coding-agent task."""
    payload = json.dumps(
        {"context_len": context_len, "task_type": task_type, "diff_size": diff_size}
    ).encode()
    request = urllib.request.Request(
        f"{ROUTER_URL}/route",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request) as response:
        return json.load(response)


# When LangChain is installed, agents can import this ready-made StructuredTool.
# The demo and MCP transport remain dependency-free.
try:
    from langchain_core.tools import StructuredTool

    langchain_tool = StructuredTool.from_function(route_coding_task)
except ImportError:
    langchain_tool = None


def run_samples() -> None:
    samples = [
        (18000, "edit", 480),
        (72000, "debug", 24),
        (12000, "review", 80),
        (18000, "edit", 480),
        (5000, "chat", 0),
        (72000, "debug", 24),
    ]
    for context_len, task_type, diff_size in samples:
        result = route_coding_task(context_len, task_type, diff_size)
        print(
            f"{task_type:7} {context_len:>6} tokens -> "
            f"{result['backend']:<22} {result['cache_status']}"
        )


def serve_mcp() -> None:
    """Serve the tool over newline-delimited JSON-RPC on stdin/stdout."""
    for line in sys.stdin:
        message = json.loads(line)
        method = message.get("method")
        if method == "tools/list":
            result = {
                "tools": [{
                    "name": "route_coding_task",
                    "description": "Select a model from coding task shape",
                    "inputSchema": {
                        "type": "object",
                        "properties": {
                            "context_len": {"type": "integer"},
                            "task_type": {"type": "string"},
                            "diff_size": {"type": "integer"},
                        },
                        "required": ["context_len", "task_type", "diff_size"],
                    },
                }]
            }
        elif method == "tools/call":
            result = route_coding_task(**message["params"]["arguments"])
        else:
            result = {"error": f"unsupported method: {method}"}
        print(json.dumps({"jsonrpc": "2.0", "id": message.get("id"), "result": result}), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--serve", action="store_true", help="serve the router tool over stdio")
    args = parser.parse_args()
    serve_mcp() if args.serve else run_samples()
