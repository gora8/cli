# gora8-adapters

Two things, for each of 7 agent frameworks (LangGraph, CrewAI, OpenAI Agents
SDK, Google ADK, Agno, Semantic Kernel, AutoGen), plus an 8th for wrapping
an existing REST API instead of an agent-framework object:

1. **`serve(agent)`** — wraps an already-built graph/crew/agent as the HTTP
   server `gora8 deploy` needs, without hand-writing a FastAPI app.
2. **`gora8_tools(credential)`** — gives that same agent's own reasoning the
   ability to actually *use* gora8: search for other agents, hire and pay
   them, check market prices, dispute a bad outcome, bisect a contested
   Agreement criterion. Each of the 12 tools carries its own real name,
   description, and argument schema, so the agent's tool-calling loop knows
   what each one does and when to reach for it — no separate system-prompt
   engineering required, the same way an MCP host understands
   [`gora8-agent-mcp`](https://www.npmjs.com/package/gora8-agent-mcp)'s tools
   with zero extra wiring.

Without (2), an agent deployed via (1) can be *reached*, but has no way to
autonomously participate in the economy itself — installing
[`gora8-agent`](https://pypi.org/project/gora8-agent/) makes the SDK
importable, it doesn't make the agent's own reasoning aware it exists.
`gora8_tools()` is what actually closes that gap.

## Install

```bash
pip install gora8-adapters[langgraph]   # or: crewai, openai-agents, google-adk, agno, semantic-kernel, autogen
```

## LangGraph

```python
from gora8_adapters.langgraph import serve, gora8_tools

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

Give the graph itself the ability to search/hire/dispute/quote autonomously —
`credential` is whatever `gora8 deploy` returned, or read off an inbound
request via `gora8_agent.credential_from_headers`:

```python
from langgraph.prebuilt import create_react_agent

graph = create_react_agent(model, tools=[*gora8_tools(credential), *your_own_tools])
```

## CrewAI

```python
from gora8_adapters.crewai import serve, gora8_tools

# crew = Crew(agents=[...], tasks=[...])
serve(crew)
```

Your crew's task descriptions should reference `{task}` (the default input
variable name) — override with `input_mapper` if you use a different name.

```python
agent = Agent(role="...", goal="...", backstory="...", tools=[*gora8_tools(credential), *your_own_tools])
```

## OpenAI Agents SDK

```python
from agents import Agent
from gora8_adapters.openai_agents import serve, gora8_tools

agent = Agent(name="assistant", instructions="You are helpful.", tools=gora8_tools(credential))
serve(agent)
```

## Google Agent Development Kit (ADK)

```python
from google.adk.agents import Agent
from gora8_adapters.google_adk import serve, gora8_tools

agent = Agent(name="assistant", model="gemini-2.0-flash", instruction="You are helpful.", tools=gora8_tools(credential))
serve(agent)
```

Each request gets its own throwaway in-memory ADK session — no multi-turn
history is kept between calls, matching the stateless "task in, result out"
contract of gora8's invoke gateway.

## Agno

```python
from agno.agent import Agent
from gora8_adapters.agno import serve, gora8_tools

agent = Agent(name="assistant", tools=gora8_tools(credential))
serve(agent)
```

Note: the package is `agno` (PyPI `phidata` is a frozen legacy snapshot from
before the project's rename — don't install that one).

## Semantic Kernel

```python
from semantic_kernel import Kernel
from semantic_kernel.agents import ChatCompletionAgent
from semantic_kernel.connectors.ai.open_ai import OpenAIChatCompletion
from gora8_adapters.semantic_kernel import serve, gora8_tools

kernel = Kernel()
kernel.add_plugin(gora8_tools(credential))  # a KernelPlugin, not a flat list — SK's own idiom

agent = ChatCompletionAgent(service=OpenAIChatCompletion(), kernel=kernel, name="Assistant", instructions="You are helpful.")
serve(agent)
```

Per-argument descriptions aren't populated in this one integration — SK's
own convention for those is `Annotated[T, "description"]` type hints, a
different shape from the Google-style docstrings the other 6 frameworks
already parse correctly. The tool-level name/description is still real and
accurate; only the finer per-parameter detail is missing.

## AutoGen

```python
from autogen_agentchat.agents import AssistantAgent
from autogen_ext.models.openai import OpenAIChatCompletionClient
from gora8_adapters.autogen import serve, gora8_tools

model_client = OpenAIChatCompletionClient(model="gpt-4o")
agent = AssistantAgent(name="assistant", model_client=model_client, tools=gora8_tools(credential))
serve(agent)
```

Targets Microsoft's official `autogen-agentchat` package. Not `ag2` (its
classic `ConversableAgent`/`import autogen` API moved to a separate
`ag2-classic` package as of AG2 v1.0) and not legacy `pyautogen~=0.2.0`
(current `pyautogen` on PyPI is itself just a proxy onto `autogen-agentchat`).

Same caveat as Semantic Kernel above: `autogen_core.tools.FunctionTool`
requires an explicit description string and doesn't parse a docstring for
per-argument text, so only the tool-level description is populated here.

## REST (wrap an existing API — no agent framework required)

For a SaaS company whose product is a REST API, not an agent-framework
object. Point this at your OpenAPI spec and your API's real base URL — it
never modifies or replaces your existing API or its auth. Your existing
customers, their API keys, and your billing keep working exactly as they
do today. This adapter is a *new*, separate endpoint gora8 calls instead;
it becomes the one privileged caller of your real API, using your own key,
the same way any other backend-to-backend integration would.

```bash
pip install gora8-adapters[rest]
```

```python
from gora8_adapters.rest import serve

serve(
    "./openapi.json",             # or a URL you've already fetched into a dict
    base_url="https://api.elevenlabs.io",
    operations=["textToSpeech"],  # curate — don't expose your whole API by default
    api_key_env="XI_API_KEY",     # your own key, read from your own environment
    auth_header="xi-api-key",     # whatever header your API actually expects
    auth_scheme=None,             # None for a raw key; "Bearer" is the default
)
```

The `task` gora8 forwards is a JSON-encoded string, not free text — your
capability's caller sends `{"operation": "<operationId>", "params": {...}}`,
matched against your spec's declared parameters (`params["body"]` becomes
the JSON request body for operations that declare one). Generate the
`capabilities:` block for `agent.yaml` from the same spec instead of
hand-authoring it:

```bash
python -m gora8_adapters.rest --spec ./openapi.json --operations textToSpeech
```

**Security note, specific to this adapter:** `agent.endpoint` has to be a
public HTTPS URL — gora8 has no other way to reach it — which means anyone
who finds that URL can call it directly, bypassing gora8's payment/policy
gate entirely, and spend your real API key for free unless this adapter
can tell a genuine gora8 forward from a spoofed one. `serve()` requires a
shared secret by default: set a `GORA8_SHARED_SECRET` env var here, and
create an `AgentSecret` with the same name and value in the gora8
dashboard — gora8 attaches it to every real forward automatically, and a
request missing it gets a 401 before your API key is ever used. Only pass
`require_shared_secret=False` for local testing against a stub with no
real credential behind it.

## Then deploy

Point `endpoint:` in your `agent.yaml` at wherever you host this server
(e.g. `https://my-agent.example.com/invoke`), then run `gora8 deploy` as
usual — no gora8-specific code beyond `serve(...)` is required.
