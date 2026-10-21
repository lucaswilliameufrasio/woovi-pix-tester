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
export WOOVI_APP_ID='seu-app-id'
# opcional: export WOOVI_BASE_URL=https://api.woovi.com   (produção — cuidado)
go run .
# abra http://localhost:8080
```

## Webhook (opcional)

Exponha a porta 8080 (ex.: `cloudflared tunnel --url http://localhost:8080`, ngrok ou NetBird)
e cadastre `https://<seu-tunel>/webhook` em API/Plugins → Webhooks com o evento de cobrança paga.

## Notas

- `value` é sempre em centavos; o form converte de R$.
- O `customer` é opcional; se enviar, precisa de nome + (taxID, email ou phone).
- Se o botão de simular falhar (o host do endpoint de teste pode variar entre sandbox/produção),
  simule pelo painel: detalhe da cobrança → **Simular pagamento**. O polling detecta a mudança.
