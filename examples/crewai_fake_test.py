"""Verifies the crewai adapter's actual wiring (server, input/output mapping)
without needing a real LLM API key. Uses a stand-in object matching the real
`Crew.kickoff(inputs=...)` / `CrewOutput.raw` interface shape — everything in
gora8_adapters.crewai runs for real; only CrewAI's own LLM execution is
stubbed out.
"""


class FakeCrewOutput:
    def __init__(self, raw):
        self.raw = raw


class FakeCrew:
    def kickoff(self, inputs):
        return FakeCrewOutput(raw=f"crew processed: {inputs['task']}")


if __name__ == "__main__":
    from gora8_adapters.crewai import serve

    serve(FakeCrew())
