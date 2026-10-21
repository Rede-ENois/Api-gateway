# Enois Cloud API Gateway

Gateway reverso em Go para serviços Cloud/ML. A configuração de exemplo expõe
uma API de moderação que envia textos e sinais de comportamento ao serviço ML,
para detectar spam, texto aleatório e abuso. O gateway controla a admissão
antes de enviar chamadas ao upstream e mantém o número de goroutines em
trabalho limitado.

## Executar

```bash
cp gateway.example.yaml gateway.yaml
go run ./cmd/gateway -config gateway.yaml
```

O endpoint `GET /healthz` não passa pelo proxy. Para cada rota, o gateway aceita:

- padrões exatos (`/v1/models`), parâmetros de segmento (`/v1/models/{id}`),
  curinga de um segmento (`/users/*`), curinga recursivo (`/users/**`) e regex
  (`re:^/v1/models/[a-z]+$`). A rota mais específica vence sobre um curinga;
- Gatekeeper com um header configurável por rota (padrão `X-API-Key`) e escopos
  em `X-Scopes` (separados por espaços),
  mais um teto de chamadas simultâneas por rota;
- uma lista global `rate_limit`, com uma rota por regra e chaves `ip`,
  `api_key` ou `ip_api_key`; os padrões disponíveis são `token_bucket`,
  `fixed_window` e `sliding_window`.

Para a rota de moderação, envie ao modelo o texto e os sinais que ele espera,
por exemplo `{"text":"...","event":"comment","account_age_days":2}`.
O body é transmitido sem alterações; a decisão do modelo (por exemplo,
`allow`, `review` ou `block`) e a pontuação retornam ao cliente. O gateway
acrescenta somente os metadados confiáveis `X-Gateway-Client-IP` e
`X-Gateway-Route`, depois de remover cópias enviadas pelo cliente. Isso permite
que o modelo correlacione reincidência sem confiar em cabeçalhos falsificados.

Quando uma fila ou limite de concorrência está cheia, a chamada é recusada em
vez de criar goroutines sem limite (`503` para pool lotado e `429` para rate
limit/concurrency). Cada worker encaminha uma solicitação e aguarda a resposta
do upstream; o timeout também cancela o request ao serviço ML.

### Token bucket e rotas recursivas

Com `limit: 30`, `window: 1m` e `burst: 5`, cada chave (neste caso,
IP + credencial) começa com cinco tokens e volta a receber, em média, um token
a cada dois segundos. Cada chamada consome um token. Sem tokens, recebe `429`
com `Retry-After`; em um minuto não passam mais de 30 chamadas sustentadas por
chave. O valor de credencial usado pelo rate limit é o mesmo de
`gatekeeper.header`.

Para aplicar políticas independentes a várias árvores de rotas, use a lista
global `rate_limit`. A regra mais específica aplicável vence:

```yaml
- route: /users/** # /users/42 e /users/42/comments/7
  pattern: token_bucket
  limit: 30
  window: 1m
  burst: 5
  key: ip_api_key
- route: /users/me # prevalece sobre /users/**
  pattern: fixed_window
  limit: 10
  window: 1m
  key: ip_api_key
```

As rotas de proxy continuam em `routes`; elas definem o upstream, métodos e o
Gatekeeper. Por exemplo:

```yaml
routes:
  - name: users-moderation
    pattern: /users/**
    methods: [POST, PUT, PATCH]
    upstream: http://ml-moderation:8000
    gatekeeper:
      header: X-Client-Token
      api_keys: [replace-with-a-secret]
```

## Verificação

```bash
go test ./...
go vet ./...
```

Para produção, injete as chaves por um gerenciador de segredos ao gerar o JSON;
não faça commit delas. Os limites são locais à instância. Para um limite global
em múltiplas réplicas, substitua o armazenamento em memória por Redis ou outro
backplane atômico.

## Container

```bash
docker build -t enois-gateway .
docker run --rm -p 8080:8080 -v "$PWD/gateway.yaml:/etc/enois/gateway.yaml:ro" enois-gateway
```

O `Dockerfile` usa build multi-stage, binário estático sem símbolos, imagem
`scratch`, CA certificates para upstreams HTTPS e usuário não-root. No YAML,
`gatekeeper.header` escolhe de onde a chave vem; por exemplo, use
`header: Authorization` se o cliente enviar o token nesse header (o valor deve
corresponder exatamente a uma entrada em `api_keys`).
