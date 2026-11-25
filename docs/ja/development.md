# 開発

## 技術スタック

| 領域                   | 採用技術                                                                                |
| ---------------------- | --------------------------------------------------------------------------------------- |
| API                    | Go 1.27.4 + [Huma v2](https://huma.rocks)（OpenAPI）+ chi/v5 + [sqlc](https://sqlc.dev) |
| DB                     | MySQL 26.10.0（Docker Compose）                                                         |
| オブジェクトストレージ | S3 互換（ローカルは Versity S3 Gateway）                                                |
| Web                    | React 19 + TypeScript 7 + Vite + TanStack Router + Zustand + Tailwind CSS 4 + Luxon     |
| Lint / Format          | Biome（Web）/ gofmt（Go）                                                               |
| パッケージマネージャ   | Bun 1.4.5                                                                               |
| テスト                 | Go E2E（testify + 実 MySQL）/ Vitest（Web）                                             |

## ディレクトリ構成

```
apps/
  api/                 Go REST API
    cmd/api/           エントリポイント
    cmd/createuser/    ユーザー作成 CLI
    cmd/healthcheck/   コンテナのヘルスチェック
    cmd/importtimetree/ TimeTree からの取り込み
    cmd/openapi/       OpenAPI 記述の書き出し
    internal/          ドメインのパッケージ（auth, dav, ical, recurrence,
                       storage, timetree 等）、db/generated、
                       http/{handlers,middleware,router}
    tests/e2e/         E2E テスト
  web/                 React SPA
    src/components/     UI コンポーネント
    src/routes/         TanStack Router（file-based, share/ embed/ 含む）
    src/stores/         Zustand（auth / calendar / ui ストア）
    src/lib/            ユーティリティ（date, recurrence, upload, theme 等）
    src/i18n/           日本語 / 英語ロケール
sql/
  core/                共有スキーマ契約の vendor コピー（上流に固定、編集不可）
    tables/             共有テーブル定義
    triggers/           共有トリガ
    conformance/        契約適合チェック
    tools/              スキーマの組み立て、vendor、層の検査
    upgrades/           既存の DB の共有テーブルを今の形へ上げる SQL
  time/                本アプリ固有のテーブル
    tables/             テーブル定義
    views/              ビュー定義
    constraints/        テーブルの後に足す外部キーと制約
    triggers/           本アプリの層のトリガ
    upgrades/           既存の DB の本アプリのテーブルと行を今の形へ上げる SQL
  queries/             sqlc クエリ定義
  schema.sql           build-schema.sh で生成される結合スキーマ
  sqlc.yaml            sqlc 設定
compose.yml            MySQL + オブジェクトストレージのローカルスタック
compose.prod.yml       自己ホスト用スタック（MySQL + オブジェクトストレージ + API + Web）
```

本番スタックの構成は[デプロイ](deployment.md)を参照してください。

## テスト

E2E はテストごとに独立したユーザーとカレンダーを生成（テナント分離）し、実 MySQL に対して並列実行します。

```bash
make db-up                      # TC_DB_PORT（既定 33306）で MySQL 起動
make test-e2e                   # TC_TEST_INTEGRATION=1 で実行
make storage-up                 # storage 付きの実行が使うオブジェクトストレージ
make test-e2e-storage           # オブジェクトストレージが要るテストも実行
make test-e2e-storage-absent    # オブジェクトストレージ不在時の 503 のテスト
```

`TC_TEST_STORAGE` を付けないと、添付・アルバム・アバター・カバーのテストは skip し、`make test-e2e` はそれでも成功します。緑の結果をそのまま信じず、skip の数を確認してください。オブジェクトストレージ不在のテストは、逆に `TC_TEST_STORAGE` を付けると skip します。テストサーバーはプロセスごとに 1 回しか作られないため、両側を見るには 2 回の実行が要ります。

テストは `127.0.0.1:${TC_DB_PORT:-33306}` に接続します。CI も同じなので、既定以外の `TC_DB_PORT` を使う場合は両方のコマンドで export してください。

CI（`.github/workflows/ci.yml`）では Web（Biome / typecheck / Vitest / build）と API（core の検査、生成物の差分、conformance、upgrade path、build、vet、オブジェクトストレージありとなしの E2E）を実行します。

## マイグレーションとコード生成

- スキーマは 2 層構成です。`sql/core/` は複数のプロダクトが同じ DB に書き合うための共有スキーマ契約のコピーで、編集できず、`sql/core/UPSTREAM.json` で上流に固定されています。`sql/time/` が本アプリ固有のテーブルです。
- `sql/core/tools/build-schema.sh core time` が core、time の順に結合して `sql/schema.sql` を生成し（`make db-schema`）、`make db-apply` が空の DB に適用します。
- 既存の DB は作り直さずに更新します。手順は[既存環境の更新](deployment.md#既存環境の更新)を参照してください。`bash scripts/check-upgrade-path.sh` は、データの入った v0.1.0 の DB が更新で現在のスキーマになることを確かめます。
- `make test-conformance` は、生成したスキーマが契約を満たしているかを検証します。`make check-core` は、vendor したコピーが未編集であることと、製品層が core のテーブルを変更していないことをオフラインで確認します。`make check-core-pin` はさらに、コピーが由来を主張するコミットと一致するかを確認し、GitHub へのネットワークアクセスが必要です。
- クエリは `sql/queries/*.sql` に書き、`make sqlc` で `apps/api/internal/db/generated/` に Go コードを生成します。
