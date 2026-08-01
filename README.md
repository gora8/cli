# gora8-adapters

Wrap a LangGraph graph or CrewAI crew as an HTTP server gora8 can deploy —
without hand-writing a FastAPI app yourself.

gora8's `agentctl deploy` only needs a public HTTPS endpoint that accepts a
`POST` with `{"task": "..."}` and returns JSON. This package spins up that
endpoint for you from an already-built graph or crew.

## Install

```bash
pip install gora8-adapters[langgraph]   # or: crewai, openai-agents, google-adk, agno, semantic-kernel, autogen
```

## LangGraph

```python
from gora8_adapters.langgraph import serve

# graph = your_state_graph.compile()
serve(graph)
```

Defaults to the common `messages`-keyed state convention. If your graph uses
a different state schema, pass your own mappers:

```python
serve(
    graph,
    input_mapper=lambda task: {"my_input_key": task},
    output_mapper=lambda state: state["my_output_key"],
)
```

## CrewAI

```python
from gora8_adapters.crewai import serve

# crew = Crew(agents=[...], tasks=[...])
serve(crew)
```

Your crew's task descriptions should reference `{task}` (the default input
variable name) — override with `input_mapper` if you use a different name.

## OpenAI Agents SDK

```python
from agents import Agent
from gora8_adapters.openai_agents import serve

agent = Agent(name="assistant", instructions="You are helpful.")
serve(agent)
```

## Google Agent Development Kit (ADK)

```python
from google.adk.agents import Agent
from gora8_adapters.google_adk import serve

agent = Agent(name="assistant", model="gemini-2.0-flash", instruction="You are helpful.")
serve(agent)
```

Each request gets its own throwaway in-memory ADK session — no multi-turn
history is kept between calls, matching the stateless "task in, result out"
contract of gora8's invoke gateway.

## Agno

```python
from agno.agent import Agent
from gora8_adapters.agno import serve

agent = Agent(name="assistant")
serve(agent)
```

Note: the package is `agno` (PyPI `phidata` is a frozen legacy snapshot from
before the project's rename — don't install that one).

## Semantic Kernel

```python
from semantic_kernel.agents import ChatCompletionAgent
from semantic_kernel.connectors.ai.open_ai import OpenAIChatCompletion
from gora8_adapters.semantic_kernel import serve

agent = ChatCompletionAgent(service=OpenAIChatCompletion(), name="Assistant", instructions="You are helpful.")
serve(agent)
```

## AutoGen

```python
from autogen_agentchat.agents import AssistantAgent
from autogen_ext.models.openai import OpenAIChatCompletionClient
from gora8_adapters.autogen import serve

model_client = OpenAIChatCompletionClient(model="gpt-4o")
agent = AssistantAgent(name="assistant", model_client=model_client)
serve(agent)
```

Targets Microsoft's official `autogen-agentchat` package. Not `ag2` (its
classic `ConversableAgent`/`import autogen` API moved to a separate
`ag2-classic` package as of AG2 v1.0) and not legacy `pyautogen~=0.2.0`
(current `pyautogen` on PyPI is itself just a proxy onto `autogen-agentchat`).

## Then deploy

Point `endpoint:` in your `agent.yaml` at wherever you host this server
(e.g. `https://my-agent.example.com/invoke`), then run `agentctl deploy` as
usual — no gora8-specific code beyond `serve(...)` is required.
