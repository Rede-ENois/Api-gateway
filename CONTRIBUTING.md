# Como contribuir

Obrigado pelo interesse. Este é o gateway reverso em Go do **Énóis**, que faz admissão e roteamento
dos serviços de Cloud/ML. Antes de começar: leia o [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) e o
[`AGENTS.md`](AGENTS.md). Falha de segurança **não** vira issue — o canal está no
[`SECURITY.md`](SECURITY.md).

## Rodando

```bash
cp gateway.example.yaml gateway.yaml
go run ./cmd/gateway -config gateway.yaml
```

`GET /healthz` é respondido pelo próprio gateway e nunca é proxiado.

## Antes de abrir a PR

```bash
go build ./...
go vet ./...
go test ./... -race
```

**Nunca afirme que está verde sem prova executada.**

## Regras de código

- Erro de configuração **falha no boot**, não em runtime. Rota ou chave desconhecida é erro de
  subida, com mensagem que diga qual campo está errado.
- Toda resposta de admissão é explícita: 401/403/429 com corpo e sem vazar o motivo interno.
- Cabeçalho de confiança (`X-Gateway-Client-IP`, `X-Gateway-Route`) é sempre sobrescrito pelo
  gateway, nunca copiado do cliente.
- `err` não é descartado sem motivo escrito. Nenhum `fmt.Println` de depuração no diff.
- Dependência nova precisa de justificativa; a biblioteca padrão vence.
- Teste de rota usa `httptest`, nunca upstream externo real.
- Comentário registra restrição que o código não mostra — nunca narra o óbvio.

## Regras de pull request

Uma PR só é revisada se cumprir os seis pontos abaixo.

1. **Uma feature por PR.**
2. **Teste para cada mudança de comportamento.** Bug corrigido ganha teste que falha no código
   antigo.
3. **Documentação atualizada no mesmo PR.**
4. **Sem comentário narrando o óbvio.**
5. **`go build`, `go vet` e `go test` verdes antes do push.**
6. **Issue aberta com o plano de implementação**, referenciada pela PR.

Além disso: a PR declara o uso de IA no checklist, e o merge exige aprovação de alguém que não
empurrou o código.

## Commits

- Imperativo com escopo: `feat(routes): ...`, `fix(limiter): ...`.
- O corpo conta o que estava errado e o que muda — o diff já mostra o como.
- Descreva **como você verificou**: qual teste, quantos passaram, o que foi conferido à mão.

## Contribuições com IA

1. **Declare o uso** no checkbox do template. Sem nome de ferramenta, sem `Co-Authored-By`.
2. **Quem submete responde pelo código.** Entenda cada linha.
3. **Prova, não promessa.**
4. **Zero slop.** Sem código gerado em massa, sem dependência injustificada, sem refatoração que a
   tarefa não pediu.
5. **Segurança vem primeiro.** As regras do [`AGENTS.md`](AGENTS.md) valem integralmente.
6. **Revisão humana antes do merge continua sendo a regra.**
