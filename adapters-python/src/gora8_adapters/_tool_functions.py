"""The 16 gora8-agent Client methods, as plain functions with rich
Google-style docstrings — defined once here, converted into each
framework's own native tool format by that framework's own adapter
module (langgraph.py, crewai.py, ...).

Why this exists at all: gora8-adapters previously only wrapped an
already-built agent as a deployable HTTP server (serve()) — it never
gave that agent's own reasoning any way to actually call search/hire/
dispute/quote, and nothing else did either (gora8 deploy's SDK
auto-install makes the library importable, not something the agent's
tool-calling loop knows to reach for). The one place this was already
solved is gora8-agent-mcp: each tool is registered with a real name,
description, and schema, so any MCP host understands what it does and
when to use it with zero separate prompt engineering. This module is
that same fix, generalized past MCP — the descriptions below are kept
close to gora8-agent-mcp's own tool descriptions on purpose, so an
agent behaves the same way regardless of which surface exposed these
capabilities to it.

One function object per tool, not a generic loop that generates 16
near-identical closures — each framework's own tool decorator
introspects the function's docstring/type hints to build its schema,
and a hand-written docstring per tool produces a meaningfully better
schema (accurate per-argument descriptions) than anything a loop over a
shared template could produce.
"""

from typing import TYPE_CHECKING, Any, Callable, Optional

# Deliberately NOT `from __future__ import annotations` — this module's
# entire purpose is being introspected by several different frameworks'
# automatic schema generators, and CrewAI's (built on pydantic's
# create_model) needs live type objects, not PEP 563 deferred string
# annotations, to resolve `Optional[str]` etc. without an explicit
# model_rebuild() step nothing here would ever call. Found by testing
# against the real installed package, not assumed.

if TYPE_CHECKING:
    from gora8_agent import Client


def build_tool_functions(client: "Client") -> list[Callable[..., Any]]:
    """Returns the 16 tool functions, each a closure over `client` —
    call this once per agent (a `Client` is scoped to one
    AgentCredential) and hand the result to whichever framework-native
    conversion your adapter module provides."""

    def search(query: Optional[str] = None, capability: Optional[str] = None) -> list:
        """Find other agents by keyword and/or capability id.

        Covers both agents deployed through gora8 and any other agent
        discoverable by crawling ERC-8004's open Identity Registry — not
        a gora8-walled directory. Each result has a `source` field
        ('gora8' or 'external'): an external result has no gora8-stored
        price (pass its id as target_actor_id to the hire tool, with an
        explicit price) and no gora8-computed stats. Results are not
        ranked by gora8 — choose using the raw stats/track record
        returned per result.

        Args:
            query: Free-text search across name/description/capabilities.
            capability: Exact or partial capability id to filter on.
        """
        return client.search(query=query, capability=capability)

    def hire(
        target_agent_id: Optional[str] = None,
        target_actor_id: Optional[str] = None,
        price: Optional[float] = None,
        payload: Any = None,
        capability: Optional[str] = None,
        acceptance_criteria: Optional[list] = None,
    ) -> dict:
        """Hire and pay another agent on this agent's own authority.

        Real, on-chain settlement, gated by this agent's own Mandate —
        cannot exceed its configured spending limit, structurally,
        regardless of what this call requests. Provide exactly one of
        target_agent_id (another gora8 agent — price is looked up
        automatically) or target_actor_id (an external agent found via
        search with source='external' — price is required, since gora8
        has no stored pricing for an agent it doesn't manage). Once
        acceptance_criteria is declared, a specific criterion can later
        be bisected via file_resolution_case instead of disputing the
        whole deal — the returned agreement_id is what that needs.

        Args:
            target_agent_id: Another gora8-deployed agent's id.
            target_actor_id: An external, ERC-8004-registered agent's actor id.
            price: Required when using target_actor_id.
            payload: The task/request body to send to the hired agent.
            capability: Optional capability id being invoked.
            acceptance_criteria: Optional list of typed acceptance-criteria dicts
                (each `{"id", "description", "type": "MECHANICAL"|"SUBJECTIVE",
                "predicate"}`) this hire's Agreement can later be bisected against.
        """
        result = client.hire(
            target_agent_id=target_agent_id,
            target_actor_id=target_actor_id,
            price=price,
            payload=payload,
            capability=capability,
            acceptance_criteria=acceptance_criteria,
        )
        return {"result": result.result, "agreement_id": result.agreement_id}

    def dispute(target_agent_id: str, wallet_transaction_id: str, reason: str) -> dict:
        """File a dispute against target_agent_id over a specific past payment this agent made to it.

        wallet_transaction_id must reference a real outbound payment this
        agent made (from a prior hire call) — a claim about a payment
        that never happened is rejected, not just unsigned.

        Args:
            target_agent_id: The agent that was paid and is being disputed.
            wallet_transaction_id: The id of the past outbound payment.
            reason: Why this outcome is being disputed.
        """
        return client.dispute(target_agent_id=target_agent_id, wallet_transaction_id=wallet_transaction_id, reason=reason)

    def plan(
        target_agent_id: Optional[str] = None,
        target_actor_id: Optional[str] = None,
        capability: Optional[str] = None,
        price: Optional[float] = None,
        payload: Any = None,
        acceptance_criteria: Optional[list] = None,
        max_spend: Optional[float] = None,
        deadline_seconds: Optional[int] = None,
    ) -> dict:
        """Assemble a set of executable, policy-checked options for a goal, without committing to or paying any of them yet.

        Provide exactly one of target_agent_id, target_actor_id, or
        capability — capability discovers multiple candidates via
        ERC-8004 plus a live quote probe against each reachable one,
        instead of naming a single target. Check each option's
        policy.permitted before choosing it — an unpermitted option is
        still returned (with why), not silently hidden. Pass the chosen
        option's option_id to the commit tool next.

        Args:
            target_agent_id: Another gora8-deployed agent's id.
            target_actor_id: An external, ERC-8004-registered agent's actor id.
            capability: Discovers multiple candidates instead of naming one target.
            price: Required when using target_actor_id.
            payload: The task/request body that will be sent if this plan is executed.
            acceptance_criteria: Optional list of typed acceptance-criteria dicts.
            max_spend: Filters out options priced above this amount.
            deadline_seconds: How long the plan and its options stay valid (default 300).
        """
        return client.plan(
            target_agent_id=target_agent_id,
            target_actor_id=target_actor_id,
            capability=capability,
            price=price,
            payload=payload,
            acceptance_criteria=acceptance_criteria,
            max_spend=max_spend,
            deadline_seconds=deadline_seconds,
        )

    def commit(plan_id: str, option_id: str) -> dict:
        """Lock in one option from a prior plan call.

        Forms the buyer-signed Agreement for a gora8-internal target
        (best-effort, same as hire). Doesn't move any money yet; call
        execute next with the same plan_id/option_id to actually settle.

        Args:
            plan_id: The plan id returned by a prior plan call.
            option_id: The chosen option's id from that plan's options list.
        """
        return client.commit(plan_id, option_id)

    def execute(plan_id: str, option_id: str) -> dict:
        """Re-validate a committed plan option fresh and, only if the forward call to the target succeeds, settle payment.

        Re-checks authority, balance, and that the target's on-file
        wallet hasn't changed since plan/commit. Fails closed on an
        expired plan or a policy check that no longer passes.

        Args:
            plan_id: The plan id from a prior plan call.
            option_id: The option id that was passed to commit for this plan.
        """
        result = client.execute(plan_id, option_id)
        return {"result": result.result, "agreement_id": result.agreement_id}

    def verify(agreement_id: str) -> dict:
        """Standalone, read-only delivery-criteria check for a finalized Agreement.

        Unlike file_resolution_case, this never creates a ResolutionCase
        or draws a panel. MECHANICAL criteria resolve deterministically;
        SUBJECTIVE reports requires_dispute (file one via
        file_resolution_case to actually resolve it); ATTESTED reports
        unsupported.

        Args:
            agreement_id: The Agreement id to check delivery criteria for.
        """
        return client.verify(agreement_id)

    def quote(target_actor_id: str, payload: Any = None, capability: Optional[str] = None) -> dict:
        """Probe one external agent's real x402 challenge for what it would charge for a given payload, right now.

        No money moves — this relays the counterparty's own 402
        challenge back to you. Request-specific, not a standing price
        (see the returned comparability field).

        Args:
            target_actor_id: The external agent's ERC-8004 actor id to probe.
            payload: The request body that would be sent if hired.
            capability: Optional capability id.
        """
        return client.quote(target_actor_id, payload=payload, capability=capability)

    def quotes(candidates: list, payload: Any = None, capability: Optional[str] = None) -> list:
        """Collect quotes from up to 10 candidates in one call.

        Each candidate needs exactly one of target_agent_id/
        target_actor_id. A gora8-deployed candidate returns its own
        stored, standing price (comparability: INDICATIVE); an external
        actor returns a live probe (comparability: REQUEST_SPECIFIC) —
        don't compare the two without checking comparability first.

        Args:
            candidates: List of dicts, each with target_agent_id or target_actor_id.
            payload: The request body that would be sent if hired.
            capability: Optional capability id.
        """
        return client.quotes(candidates, payload=payload, capability=capability)

    def price_reference(capability: str) -> dict:
        """Aggregate price stats (min/max/mean/median) across gora8-native agents publishing a given capability.

        External, ERC-8004-crawled agents have no gora8-stored pricing
        yet, so they're not counted.

        Args:
            capability: The capability id to look up aggregate pricing for.
        """
        return client.price_reference(capability)

    def history_with(counterparty_id: str) -> dict:
        """This agent's own bilateral transaction history with one counterparty.

        Args:
            counterparty_id: Either a gora8 Agent id or an external ERC-8004 actor id.
        """
        return client.history_with(counterparty_id)

    def get_agreement(agreement_id: str) -> dict:
        """Fetch a signed Agreement by id, independently verifiable against its own stored termsHash.

        Includes its declared acceptance_criteria, delivered_at, and
        challenge_deadline.

        Args:
            agreement_id: The Agreement id, typically from a prior hire's result.
        """
        return client.get_agreement(agreement_id)

    def file_resolution_case(agreement_id: str, criterion_id: str, reason: str) -> dict:
        """Bisect one declared acceptance criterion of a finalized Agreement this agent bought under, instead of disputing the whole deal.

        Only the Agreement's buyer may file, only within its
        challenge_deadline. MECHANICAL criteria resolve immediately in
        the response; SUBJECTIVE criteria draw a panel and resolve
        later — poll with get_resolution_case.

        Args:
            agreement_id: The Agreement id this criterion belongs to.
            criterion_id: The id of the specific declared criterion being disputed.
            reason: Why this criterion wasn't met.
        """
        return client.file_resolution_case(agreement_id, criterion_id, reason)

    def list_resolution_cases(agreement_id: str) -> list:
        """List every ResolutionCase filed against one Agreement.

        Args:
            agreement_id: The Agreement id to list cases for.
        """
        return client.list_resolution_cases(agreement_id)

    def get_resolution_case(resolution_case_id: str) -> dict:
        """Fetch one ResolutionCase's current status by id.

        Args:
            resolution_case_id: The ResolutionCase id to check.
        """
        return client.get_resolution_case(resolution_case_id)

    def appeal_resolution_case(resolution_case_id: str) -> dict:
        """Appeal the last-settled tier of a SUBJECTIVE ResolutionCase's ruling.

        Only available to the side that just lost that tier, only
        within its appeal window, and only up to maxAppealTier. Draws a
        fresh, larger-bonded panel for the new tier.

        Args:
            resolution_case_id: The ResolutionCase id whose last ruling is being appealed.
        """
        return client.appeal_resolution_case(resolution_case_id)

    return [
        search,
        hire,
        dispute,
        plan,
        commit,
        execute,
        verify,
        quote,
        quotes,
        price_reference,
        history_with,
        get_agreement,
        file_resolution_case,
        list_resolution_cases,
        get_resolution_case,
        appeal_resolution_case,
    ]


def short_description(fn: Callable[..., Any]) -> str:
    """The first paragraph of `fn`'s docstring (before the blank line
    that precedes its Args: section) — for the frameworks whose tool
    constructor wants an explicit, separate description string rather
    than parsing the full docstring itself."""
    doc = (fn.__doc__ or "").strip()
    return doc.split("\n\n")[0].strip()
