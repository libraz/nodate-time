# デプロイ

`compose.prod.yml` が、MySQL・オブジェクトストレージ・API・ビルド済み SPA を 1 台のホスト上でまとめて動かします。イメージは pull せずその場でビルドします。SPA は API のオリジンをバンドルに焼き込むため、2 つのデプロイ先に対して正しいビルド済みイメージは存在しません。

![アーキテクチャ](../images/architecture.svg)

ホスト名 1 つ、証明書 1 枚で、公開するポートはそれだけです。入口は web サービスのみで、TLS の終端、SPA の配信、`/api` の API への中継（compose ネットワーク越し）を担います。API もオブジェクトストレージもインターネットからの経路を必要とせず、与えてもいません。添付・アルバム写真・アバターは API が中継します。ブラウザには API 自身へ戻る短命の署名付き URL を渡し、バイトはその上をコピーして流します。

## TLS 証明書

証明書はスタックを起動する前に存在している必要があります。証明書のない server ブロックを nginx が読み込まないためです。ポート 80 を空けた状態で先に取得します。

```sh
mkdir -p acme
certbot certonly --standalone -d app.example.com
```

更新は稼働中のサーバが `acme/` から応答するので、止めるものは何もありません。ただし nginx は再読み込みを指示されるまで古い証明書を持ち続けます。

```sh
certbot renew --webroot -w /path/to/acme \
  --deploy-hook 'docker compose -f /path/to/compose.prod.yml exec web nginx -s reload'
```

`TC_API_PUBLIC_URL` には `/api` の prefix が必要です（`https://app.example.com/api`）。OAuth のコールバックが戻ってくる先であり、blob URL が指し戻す先であり、レスポンスの `$schema` リンクを組み立てる元でもあるためです。`TC_TRUSTED_PROXIES` の既定は、Docker が compose ネットワークに割り当てるレンジ `172.16.0.0/12` です。ネットワークがその外に置かれた場合は、レンジを自分で設定してください。全クライアントが nginx を通って届くので、合っていないとレート制限が 1 バケットに潰れます。

TLS を別の場所で終端したい場合（ホスト上のリバースプロキシやトンネルなど）は、`web` サービスから TLS テンプレートのマウントと証明書の 2 つのボリュームを外し、`127.0.0.1:8081:80` を publish します。イメージは平文 HTTP で応答し、`/api` は同じように自分で API へ流すので、ブラウザから見た構成は上と変わりません。ホスト側のプロキシはその 1 ポートだけを向ければ足ります。

## 設定

設定は `compose.prod.yml` と同じ場所の `.env` に置きます。`TC_DB_ROOT_PASSWORD`・`TC_DB_PASSWORD`・`TC_JWT_SECRET`（`openssl rand -hex 32`）・`TC_WEB_URL`・`TC_API_PUBLIC_URL`・`TC_PUBLIC_HOST`・`TC_S3_ACCESS_KEY`・`TC_S3_SECRET_KEY` には安全な既定値がなく、未設定なら Compose が該当する変数名を出して停止します。`TC_SMTP_HOST` は省略できます。なければメール送信はオフで、パスワード再設定とメール確認は提供されません。[設定](configuration.md)の表のその他の変数も、同じ既定値のまま渡ります。例外は次のとおりです。

- `TC_CORS_ALLOWED_ORIGINS` は常に `TC_WEB_URL` になります。`.env` に書いた値は無視されます。
- `TC_PORT` は渡しません。API はコンテナ内で 8080 を待ち受け、ホスト側の loopback ポートは `TC_API_PORT`（既定 8080）で決めます。
- `TC_METRICS_ADDR` の既定は `:9464` です。コンテナはホストの loopback にバインドできないためです。`127.0.0.1` に閉じているのは公開側のポート（`TC_METRICS_PORT`、既定 9464）です。
- `TC_ENV` は常に `production`、`TC_DB_DSN` は `TC_DB_USER`・`TC_DB_PASSWORD`・`TC_DB_NAME` から組み立てられます。

`TC_SECRETS_KEY` は任意です。設定しない場合、管理画面は OAuth のクライアントシークレットを平文で保存する代わりに、保存自体を拒否します。

web サービスが返すページにはすべて、フレーム埋め込みを禁じる Content-Security-Policy、`nosniff`、`Referrer-Policy: no-referrer` が付き、TLS を自分で終端する場合は HSTS も付きます。例外は `/embed/*` で、どのサイトからでもフレームに入れられます。ポリシーが許すスクリプトと接続先は配信元と同じオリジンだけなので、別オリジンで API を配信する場合は `apps/web/nginx/security-headers.conf` の `connect-src` を広げてください。

## スタックの起動

```sh
docker compose -f compose.prod.yml up -d --build
docker compose -f compose.prod.yml run --rm --entrypoint /createuser api \
  -email you@example.com -password 'choose-a-long-one' -name "Your Name" -admin
```

スキーマは MySQL のボリュームを最初に作ったときに適用されます。`sql/schema.sql` は全テーブルを DROP してから作り直すので、データの入った DB には流し直しません。スキーマの変更を既存の DB に当てる方法は[既存環境の更新](#既存環境の更新)を参照してください。

## バックアップ

永続データは `mysql_data` と `storage_data` の 2 つのボリュームに入っています。バックアップは必ず両方を揃えて取ります。添付ファイルは、片方の行がもう片方のオブジェクトを指す形で成立しているためです。オブジェクトは普通のファイルで、content type と ETag は拡張属性に入っています。ファイル単位でコピーするなら拡張属性も残してください（GNU `tar --xattrs --xattrs-include='user.*'`。busybox の tar は落とします）。

データベースだけの論理ダンプは次のとおりです。

```sh
docker compose -f compose.prod.yml exec -T mysql \
  sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction "$MYSQL_DATABASE"' \
  > backup.sql
```

## 既存環境の更新

先に[バックアップ](#バックアップ)のとおり 2 つのボリュームを保存します。データの入った DB は作り直しません。`sql/schema.sql` も `sql/core/tools/build-schema.sh` も、作るテーブルをすべて DROP してから作り直すので、稼働中の DB に流すとデータが消えます。既存の DB は更新用の SQL で上げます。API を止めたうえで、次の順に当てます。

1. `sql/core/upgrades/*.sql` を、番号順にすべて。他のプロダクトと共有するテーブルを変更するもので、必ず最初に当てます。これが済むまで次の手順は実行を拒否します。
2. `sql/time/upgrades/from-<リリース>.sql` を、今のリリースから移行先までの各リリースについて古い順に。
3. 適合性検査。結果が契約どおりになっているかを確かめます。

```sh
docker compose -f compose.prod.yml stop api
docker compose -f compose.prod.yml cp sql mysql:/tmp/upgrade-sql
docker compose -f compose.prod.yml exec -T mysql bash -euc '
  export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
  for f in /tmp/upgrade-sql/core/upgrades/*.sql /tmp/upgrade-sql/time/upgrades/from-v0.1.0.sql; do
    echo "applying ${f#/tmp/upgrade-sql/}"
    mysql -uroot "$MYSQL_DATABASE" < "$f"
  done
  bash /tmp/upgrade-sql/core/conformance/run.sh \
    --dsn "root:$MYSQL_ROOT_PASSWORD@127.0.0.1:3306/$MYSQL_DATABASE" --mode schema
  rm -rf /tmp/upgrade-sql
' && docker compose -f compose.prod.yml up -d --build
```

パスワードとデータベース名はコンテナの中で、`.env` から渡された値として読まれるので、ホスト側で export する必要はありません。ループは失敗したファイルで止まり、再ビルドはすべてのファイルと検査が成功したときだけ走ります。旧版の API は先に止めてください。更新が済んだあとに旧版が削除した行は、確認待ちのアップロードに見えてしまいます。各ステップは現状を確かめてから動くので、途中で止まったものは、新しい API がまだリクエストを処理していない限り、もう一度流せます。

### 許可したメールアドレスがあるとサインインの制限が有効になる

更新では、管理画面で許可したメールアドレスを、DB が他のプロダクトと共有するサインイン許可リストへ移します。このリストに有効な行が 1 つでもあれば、`TC_GOOGLE_ALLOWED_DOMAINS` の設定の有無にかかわらず、OAuth でサインインできるのはリストにあるアドレスとドメインだけになります。ドメインのリストを空にしたまま、例外としていくつかのアドレスを登録していたデプロイでは、更新前は誰でもサインインできましたが、更新後はそのアドレスしか入れません。意図しない場合は、管理画面で該当の行を取り下げてください。

### 一度だけ書き換わる行

既存の行のうち、次のものは更新時に一度だけ書き換わります。

- アクティビティログの記録は、他のプロダクトと共通のイベント名と payload のキーになります。
- メールアドレスは小文字で保存されます。
- すべてのセッションに、本アプリが発行したという印が付きます。
- ストレージの参照数は数え直されます。
- 毎週の繰り返しは、これまで表示していた日曜始まりの週のまま残ります。

### v0.1.0 から上げる場合

配備の形も変わっています。

- web サービスが TLS を終端し、`127.0.0.1:8081` の代わりに 80 と 443 を公開します。起動前に [TLS 証明書](#tls-証明書)の手順で証明書を取得するか、ホスト側のプロキシを使い続けるなら、TLS を別の場所で終端する場合の段落に従ってください。
- 証明書を発行するホスト名 `TC_PUBLIC_HOST` が必須になりました。
- `TC_SMTP_HOST` と `TC_SMTP_FROM` は省略できます。SMTP リレーがなければメール送信はオフで、パスワード再設定とメール確認は提供されません。
- `TC_TRUSTED_PROXIES` の既定値が、Docker が compose のネットワークに割り当てる範囲 `172.16.0.0/12` になりました。
- オブジェクトストレージが MinIO（`minio` サービス、`minio_data` ボリューム）から Versity S3 Gateway（`storage` サービス、`storage_data` ボリューム）に変わりました。保存形式が違うのでボリュームは流用できず、S3 経由でオブジェクトを移します。手順は[MinIO からの移行](#minio-からの移行)を参照してください。

## オブジェクトストレージ

同梱のストレージは、普通のディレクトリの上で動く Versity S3 Gateway です。単一ノードで、erasure coding もバージョニングもレプリケーションもありません。耐久性は下のボリュームと上記のバックアップに依存し、データベースと同じ足場の上にあります。プライベートネットワークにしか応答しません。`TC_S3_*` を任意の S3 互換エンドポイントに向ければ差し替えられ、設定より上は何も変わりません。どちらの場合も、ストレージに触るのは API で、ブラウザではありません。

### MinIO からの移行

切り替え前のデプロイは、オブジェクトを MinIO 独自の形式で `minio_data` に持っているので、S3 経由でコピーします。新しい compose ファイルに `minio` サービスはもうありませんが、旧コンテナは動き続けるので、その中の `mc` を使います。上記のとおり API を止めて更新用の SQL を当て、再ビルドはオブジェクトを移し終えるまで待ちます。

```sh
B=nodate-time   # TC_S3_BUCKET
docker compose -f compose.prod.yml up -d --wait storage
OLD=$(docker ps -q --filter label=com.docker.compose.service=minio)
docker exec -e B="$B" "$OLD" sh -c '
  mc alias set old http://localhost:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc alias set new http://storage:7070 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc mb --ignore-existing "new/$B" &&
  mc mirror --preserve "old/$B" "new/$B"'
echo "SELECT storage_key, content_type FROM storage_objects WHERE storage_key LIKE 'albums/%.thumb'" |
  docker compose -f compose.prod.yml exec -T mysql \
    sh -c 'exec mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"' |
  while read -r key type; do
    docker exec -e B="$B" -e P="${key%.thumb}" -e T="$type" "$OLD" sh -c '
      mc cat "old/$B/$P/thumb" > /tmp/thumb &&
      curl -sf --aws-sigv4 aws:amz:us-east-1:s3 --user "$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD" \
        -H "Content-Type: $T" -T /tmp/thumb "http://storage:7070/$B/$P.thumb"' </dev/null ||
      echo "not copied: $key"
  done
docker compose -f compose.prod.yml up -d --build --remove-orphans
```

サムネイルには 2 段目が必要です。v0.1.0 はサムネイルを写真の下（`<photo>/thumb`）に置いていました。新しいストレージは写真本体と並べてこのキーを持てず、MinIO は別のオブジェクトの下にあるキーを一覧に出さないので、`mc mirror` からは見えません。更新用の SQL がその行をすでに `<photo>.thumb` に書き換えているので、ループはその一覧を読み、1 件ずつ新しいキーへコピーします。`not copied` と出たものは、旧ストレージに中身がなかったサムネイルです。

どちらのストレージも同じ `TC_S3_*` 認証情報を受け取るので、旧コンテナ自身の環境変数がそのまま両方に使えます。`minio_data` ボリュームは、稼働中のアプリで添付・アルバム写真・アバターを確かめるまで残し、そのあと `docker volume rm` で消します。
