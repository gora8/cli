"""Minimal LangGraph graph for verifying the adapter end to end.

Not an LLM agent — just echoes the last message back uppercased, so this
test doesn't need API keys.
"""

from typing import TypedDict, Annotated
from operator import add

from langgraph.graph import StateGraph, END


class State(TypedDict):
    messages: Annotated[list, add]


def echo_node(state: State) -> dict:
    last = state["messages"][-1]
    content = last["content"] if isinstance(last, dict) else last.content
    return {"messages": [{"role": "assistant", "content": content.upper()}]}


builder = StateGraph(State)
builder.add_node("echo", echo_node)
builder.set_entry_point("echo")
builder.add_edge("echo", END)
graph = builder.compile()

if __name__ == "__main__":
    from gora8_adapters.langgraph import serve

    serve(graph)
