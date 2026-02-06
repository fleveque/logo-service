# logo-service

Self-hosted Go microservice for stock ticker logo acquisition, processing, and serving.

Replaces Brandfetch CDN in the [dividend-portfolio](https://github.com/fleveque/dividend-portfolio) app with a 3-layer logo pipeline:

1. **Cache** — filesystem + SQLite metadata
2. **GitHub repos** — bulk import from open-source ticker logo collections
3. **LLM** — Claude/OpenAI with web search to find logos for missing tickers

## Quick Start

```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your API keys
make run
```

## API

```
GET  /healthz                          # Health check
GET  /api/v1/logos/:symbol?size=m      # Get logo PNG
POST /api/v1/admin/import?source=all   # Trigger bulk import
GET  /api/v1/admin/stats               # Logo statistics
```
