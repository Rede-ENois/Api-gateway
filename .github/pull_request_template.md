## O que muda

<!-- 2 a 5 linhas: o problema e o que a mudança faz -->

Closes #<!-- issue de origem: toda PR tem uma -->

## Como foi verificado

<!-- Prova, não promessa: comandos rodados, quantos testes passaram, o que foi conferido à mão -->

- `go build ./... && go vet ./... && go test ./... -race` → <!-- verde/vermelho -->
- conferido à mão: <!-- o quê -->

## Risco e rollback

<!-- admissão, rate limit, rota pública? como desfazer? -->

## Checklist

- [ ] Uma feature por PR — sem refactor ou correção alheia pegando carona
- [ ] Issue de origem aberta com o plano de implementação, referenciada acima
- [ ] `go build`, `go vet` e `go test` verdes
- [ ] Teste de prova incluído: falha no código antigo, passa no novo
- [ ] Sem stub, placeholder, TODO fingindo conclusão ou fallback silencioso
- [ ] Sem asserção enfraquecida, sem `t.Skip` escondido
- [ ] Cabeçalho de confiança continua sendo sobrescrito pelo gateway
- [ ] Sem segredo, token ou dado pessoal no diff
- [ ] Sem detalhe de correção de segurança no texto público (commit e PR)
- [ ] Documentação atualizada (se mudou)
- [ ] Autorrevisão feita: li o `git diff` linha a linha e entendo cada linha
- [ ] AI tools helped write this change (check if yes; no tool name or attribution line needed)
