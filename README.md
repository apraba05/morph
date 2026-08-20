# Coding-Agent Model Router with Latency Cache

Building a lightweight, GitOps-deployable routing layer for LLM inference that classifies requests (context length, task type) and dispatches to the right backend, with caching and metrics.

**Live demo:** https://morph.ashanpraba.com

The demo runs entirely in the browser against seeded data — no API keys,
no accounts, and no external services required.

## Stack

- Go
- Redis
- Kubernetes (kind)
- Helm
- Python
- LangChain
- MCP

## How it works

- Write a Go HTTP service with a /route endpoint accepting {context_len, task_type, diff_size}.
- Simple routing rules: large context_len -> 'long-context-model', edit-type tasks -> 'fast-apply-model', else default; return chosen backend + simulated latency.
- Cache each routing decision + latency sample in Redis, expose a /metrics endpoint computing p50/p99 from recent samples.
- Write a Helm chart (deployment, service, configmap for routing rules) and deploy to a local kind cluster.
- Write a small Python/LangChain script exposing the router as an MCP tool, send 5-6 sample coding-agent requests through it.
- Screen-record: kubectl apply, requests hitting the router, cache populating in Redis, /metrics showing p50/p99.

## Running locally

```bash
cd src
bash run.sh
```

Then open the printed URL. A prebuilt static version of the UI lives in
`src/web/` and can be opened directly with no server.
