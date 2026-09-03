---
name: gora8-commerce
description: Let an AI agent discover counterparties, evaluate trust evidence, plan and commit to a deal, execute payment, verify delivery, and dispute a bad outcome — autonomously, within its owner's Mandate. Use when an agent needs to hire, pay, or transact with another agent or service, or check whether a past deal was honored. Also the gora8-side half of Internet Court alignment: an agent already routing through Internet Court's six-layer commerce model can land here for the discovery/agreement/payment/execution layers without changing how it reasons.
---

# gora8 Commerce Skill

This is the discoverable invocation contract for gora8's agent-facing
product. A skill document alone is not the
product; this describes the same stable verb surface exposed through the
channels below, so an agent (or the framework/host running it) can find
and use it without prior integration work.

Full request/response shapes for every endpoint below: **`REFERENCE.md`**
in this directory. This file is the routing/decision layer; that file is
the wire contract.

## What this skill does

Gives an autonomous agent a bounded market interface: discover
counterparties, compare evidence-backed options, commit to and execute a
transaction, verify delivery, and dispute a bad outcome — all gated by
the agent's own gora8 Mandate (signed spending limits, counterparty
restrictions), enforced independently at two layers (the API and the
agent's own signer) so no single compromised component can move money
outside what the owner authorized.

gora8 does not choose a counterparty or a price for the agent. It
supplies verified facts and policy-permitted options; the agent decides.

## Relationship to Internet Court

If you (the agent, or the framework routing you) arrived here via
Internet Court's routing table: gora8 is not one of the 91 vendored
skills in that ecosystem — it's an **Internet Court-aligned execution
runtime**. That means gora8 operationalizes the same six-layer commerce
flow Internet Court describes, through direct integrations with
selected, executable underlying standards and providers where gora8
already has them — not a certified or formal implementation of
"the Internet Court protocol" (Internet Court isn't a single protocol
with a compliance suite to implement against; see `docs/research/
internet-court/README.md`), and not every layer or vendor in that
ecosystem, only a coherent default stack (see `docs/architecture/
commerce-capability-matrix.md`'s launch doctrine).

| Internet Court layer | gora8 verb(s) | Real standard used |
|---|---|---|
| 1. Discovery / identity / reputation | `discover` | ERC-8004 Identity Registry (`ERC8004Adapter`) |
| 2. Negotiation | `quote` | **Not negotiation** — x402 live probe (`X402QuoteAdapter`), price discovery and bounded quote collection only. Advisory, never binding, and there's no offer/counter-offer state machine (intent, counter-offer, expiry, terms versioning, acceptance/rejection) behind it yet. Don't describe this as "gora8 supports negotiation" |
| 3. Contracts / obligations | `plan` → `commit` | gora8-native Agreement/EconomicRecord (`Gora8NativeAgreementAdapter`) — buyer and provider signatures both cover the same canonical `termsHash`, re-checked at `execute()` time so a committed plan can't silently settle against different terms than it formed |
| 4. Payment / escrow | `execute` | x402/USDC settlement (`X402SettlementAdapter`); real on-chain collateral via `BondVault.sol` where an agent has posted a bond |
| 5. Execution | `execute` (the forward-call itself) | Direct HTTPS call to the target agent's endpoint |
| 6. Verification / disputes | `verify`, `dispute` | Mechanical criteria check; gora8-panel (default) or Kleros (unilateral escalation, real on-chain arbitration) |

There is no Internet Court backend to call — it's an agent-facing Agent
Skill / routing document, not a service. "Alignment" means
operationalizing the same model through gora8's own adapters, not
integrating against an API that doesn't exist. See
`docs/architecture/internet-court-alignment.md` for the full matrix and
`docs/research/internet-court/README.md` for the primary-source finding
this is based on.

## Stable verbs

| Verb | What it does |
|---|---|
| `discover` / `search` | Find counterparties by capability, across gora8-native agents and the open ERC-8004 Identity Registry |
| `quote` | Probe one external agent's real x402 challenge for what it would charge, right now — no money moves |
| `evaluate` | Read verified trust facts (identity age, collateral, dispute history, Mandate limits) against caller-supplied requirements |
| `plan` | Assemble a set of executable, policy-checked options for a goal — the Economic Decision Packet |
| `commit` | Lock in one option, forming the buyer-signed Agreement |
| `execute` | Re-validate fresh and settle payment against a committed option |
| `verify` | Check declared delivery criteria, deterministically where possible |
| `dispute` | File a claim against a past payment or a specific Agreement criterion, routed to the adjudicator selected at commit time |

`hire()` remains a single-call convenience wrapper equivalent to
`plan → commit → execute` for the common case of "I already know who I'm
hiring." Both paths are real and callable today; see
`MAINNET_GO_LIVE_CHECKLIST.md` for exactly how they relate under the
hood — that detail doesn't change how either is invoked.

## How to invoke this skill

- **MCP** — `agent-mcp` (npm: `gora8-agent-mcp`) exposes every verb above
  plus `hire`, `quotes`, `price_reference`, `history_with`,
  `get_agreement`, `file_resolution_case`, `list_resolution_cases`,
  `get_resolution_case`, `appeal_resolution_case` as MCP tools over
  stdio — works with any MCP-native host (Claude Desktop, Claude Code,
  any other MCP client).
- **TypeScript/JavaScript SDK** — `gora8-agent` (npm) exposes the same
  surface as typed `Client` methods (`client.plan()`, `client.commit()`,
  ...).
- **Python** — `adapters-python`'s framework adapters wrap the same HTTP
  API for LangGraph, CrewAI, OpenAI Agents SDK, Google ADK, AutoGen,
  Agno, and Semantic Kernel.
- **Direct HTTP** — every verb is a plain REST call under
  `https://api.gora8.com/v1` (or your own deployment's base URL); see
  `REFERENCE.md` for the full route list with real request/response
  bodies.

## Authentication

Two directions, same token:

- **Inbound** (gora8 forwarding a call to your agent's endpoint): read
  the header `x-gora8-agent-credential` off the request your handler is
  already processing. See `agent-ts`'s `credentialFromHeaders()` helper.
- **Outbound** (your agent calling gora8's own `/v1/...` API — `plan`,
  `commit`, `execute`, `quote`, `dispute`, etc.): send
  `Authorization: Bearer <that same credential>`. It's literally the
  same short-lived token (`<agentId>.<exp>.<hmac>`, 15-minute TTL) —
  reuse it directly, there's no separate outbound credential to obtain.
  It expires 15 minutes after mint; if a multi-step flow (`plan` →
  `commit` → `execute`) runs longer than that, get a fresh one from the
  next inbound call rather than retrying with a stale token.
- Owners (not agents) authenticate the same routes with
  `Authorization: Bearer gora8_sk_...` (an account-level API key, not an
  agent credential) — see `gora8 auth login`.

## Enforcement — what actually stops a disallowed action

Every adapter behind these verbs is labeled with one of four honest
classifications (`ENFORCED_ON_CHAIN`, `ENFORCED_BY_RUNTIME`,
`ADVISORY_ONLY`, `UNSUPPORTED`) — see `api/src/adapters/types.ts`'s
`EnforcementClassification` and `docs/architecture/
commerce-capability-matrix.md` for the full table. The two an agent
should reason about before relying on them:

- **Quotes are `ADVISORY_ONLY`.** Nothing stops a counterparty quoting
  one price via `quote` and charging another at `execute` time — the
  adapter reports what was quoted, it doesn't hold either side to it.
  Re-check the actual settled amount, don't just trust the earlier quote.
- **Collateral is real only when a bond is actually posted.** gora8's
  on-chain `BondVault` enforces locked collateral once an agent has
  posted a bond (`ENFORCED_ON_CHAIN` for that agent's disputes); an
  agent with no posted bond has no on-chain-enforced collateral backing
  it, regardless of what its profile displays. Check `custodyModel` on
  the counterparty's passport before treating "has collateral" as true.
- **A Kleros ruling being on-chain isn't the same as the remedy being
  trustless.** Kleros's own arbitration contracts produce a real,
  independently-verifiable ruling — but applying it (slashing a bond,
  transferring funds) goes through gora8's own scheduled poll and a
  controller-signed transaction, not an automatic contract-level hook.
  Classified `ENFORCED_BY_RUNTIME` for exactly this reason: an outage or
  bug in gora8's own remedy step, not Kleros's ruling, is the realistic
  failure mode.

## What this skill does not do

It does not replace agent judgment (no autonomous counterparty
selection beyond what the owner's policy permits), does not claim
support for delivery criteria with no live verification mechanism
(`ATTESTED` acceptance criteria are rejected outright — no ERC-8004
`ValidationRegistry` is deployed anywhere yet), and does not move money
outside the agent's signed Mandate, structurally, regardless of what a
caller requests.
