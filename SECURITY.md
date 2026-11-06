# Política de segurança

## Como relatar

**Não abra issue pública para vulnerabilidade.**

Escreva para **seguranca@enois.ia.br** com a versão ou commit afetado, uma reprodução mínima, o
comportamento observado versus o esperado e o impacto estimado. A triagem inicial acontece em até 5
dias úteis; a divulgação é coordenada por até 90 dias, e o advisory sai junto com o lançamento da
correção.

## Escopo

O gateway é a primeira barreira do tráfego de serviços externos. A superfície de segurança é:

- **Cabeçalhos de confiança.** `X-Gateway-Client-IP` e `X-Gateway-Route` são escritos pelo gateway e
  sobrescrevem qualquer cópia enviada pelo cliente. Um caminho que preserve o valor do cliente
  permite falsificar origem e burlar correlação de abuso.
- **Autenticação de rota.** Rota com `api_keys` exige a credencial; rota sem chave é pública por
  decisão explícita de configuração. Uma rota que ignora a própria configuração de chave é
  vulnerabilidade.
- **Rate limit e admissão.** Limite por `ip`, `api_key` ou a combinação; um caminho que permita
  contornar o balde (chave forjada, cabeçalho injetado, rota alternativa) é vulnerabilidade.
- **Vazamento de configuração.** Segredo em log, mensagem de erro ou resposta é vulnerabilidade.
  Configuração inválida falha no boot sem imprimir chave.
- **Robustez do proxy.** Smuggling de requisição, confusão de path que faça rota privada cair em rota
  pública, e timeout ausente que permita esgotar o bulkhead.

Fora de escopo: vulnerabilidades no serviço upstream ou na aplicação clonada. Reporte ao repositório
que a possui.

## O que já está em vigor

- O repositório é público e há atacante lendo. Commit e PR dizem *o que* mudou, nunca *como* uma
  falha foi corrigida.
- Segredo nunca aparece em código, log, commit, PR ou fixture.
- Um controle de segurança ou de qualidade nunca é desligado para fazer teste passar.

## Referências

OWASP API Security Top 10 e OWASP ASVS 5.0 (nível 2 como referência mínima). Isso **não** equivale a
certificação nem substitui avaliação de segurança independente.
