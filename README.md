# woovi-pix-tester

App web mínimo (Go + HTML puro, sem dependências) para testar cobranças Pix na Woovi.

- Cria cobrança (`POST /api/v1/charge`) e mostra QR Code, copia-e-cola e link de checkout
- Consulta status a cada 3s (`GET /api/v1/charge/{id}`) até `COMPLETED` ou `EXPIRED`
- Botão **Simular pagamento** (conta de teste) via `GET /openpix/testing?transactionID=<identifier>`
- Recebe webhooks em `POST /webhook` e mostra ao vivo na página (SSE)

O AppID fica só no servidor; o navegador nunca o vê.

## Pré-requisitos

1. Conta no sandbox: https://app.woovi-sandbox.com
2. Crie uma **conta bancária de teste** e marque como padrão
   (Ajustes → Pix → Configurações avançadas → *Criar uma conta bancária de teste*)
3. Crie uma aplicação em **API/Plugins** e copie o **AppID**

## Rodar

```bash
make setup
make dev
# abra http://localhost:8080
```

O assistente pede o AppID sem exibi-lo enquanto você digita e salva as
configurações em `.env`, ignorado pelo Git e criado com permissão `0600`.
Use o AppID do sandbox; a URL padrão também aponta para o sandbox. Para
configurar novamente, remova `.env` manualmente e rode `make setup`.
Variáveis de ambiente `WOOVI_APP_ID`, `WOOVI_BASE_URL` e `PORT`, quando
definidas, têm prioridade sobre `.env`. Não use o AppID de produção para testes.

## Makefile

Os comandos abaixo facilitam o desenvolvimento e a validação local.

```bash
make help
make fmt
make test
make test-race
make vet
make lint-install  # opcional; baixa golangci-lint para bin/
make lint
make js-check
make check
make coverage
make build
```

Para preparar um artefato de implantação, sem enviar arquivos a um servidor:

```bash
make package
# Exemplo de compilação cruzada:
make package TARGET_OS=linux TARGET_ARCH=amd64
```

O pacote `.tar.gz` fica em `dist/` e contém o binário e este README. O destino e o mecanismo de implantação não estão configurados neste projeto; o pacote precisa ser enviado e configurado conforme o ambiente escolhido.

## Webhook (opcional)

Exponha a porta 8080 (ex.: `cloudflared tunnel --url http://localhost:8080`, ngrok ou NetBird)
e cadastre `https://<seu-tunel>/webhook` em API/Plugins → Webhooks com o evento de cobrança paga.

## Notas

- `value` é sempre em centavos; o form converte de R$.
- O `customer` é opcional; se enviar, precisa de nome + (taxID, email ou phone).
- Se o botão de simular falhar (o host do endpoint de teste pode variar entre sandbox/produção),
  simule pelo painel: detalhe da cobrança → **Simular pagamento**. O polling detecta a mudança.
