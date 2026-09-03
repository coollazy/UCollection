# UCollection

單租戶自架的 USDT-TRC20 代收軟體。依 xpub 為每筆訂單衍生獨立收款地址，掃描 Tron 鏈上事件判斷入帳，提供後台管理與手動歸集。

規則與架構見 [CLAUDE.md](CLAUDE.md)、[技術架構設計](docs/開發流程框架-03-技術架構設計.md)、[ADR](docs/adr/README.md)。目前進度見 [docs/進度.md](docs/進度.md)。

## 快速開始

```bash
cp .env.example .env
docker compose up --build
curl localhost:8080/healthz
```

## 開發

需要 [mise](https://mise.jdx.dev/)（`mise.toml` 已釘住 Go / golangci-lint 版本）。

```bash
mise install
make build   # 編譯
make test    # 跑測試（internal/store 的測試需要 DATABASE_URL，否則自動skip）
make lint    # golangci-lint
```
