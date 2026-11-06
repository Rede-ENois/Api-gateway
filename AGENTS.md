# AGENTS.md — contrato operacional para agentes de IA

Destilado do contrato do [KofLang](https://github.com/KofLang/Kof4j) para a realidade deste
repositório. **Este arquivo é lido por todo agente de IA antes de trabalhar aqui** — as regras valem
para agentes autônomos, sessões assistidas e contribuições humanas que usam IA.

Se uma regra deste arquivo conflitar com a pressa, a pressa perde.

---

## Estado

state: ativo

intent: nenhum-slop-em-release

constraint:

- qualidade-primeiro
- nenhum-bug-em-release
- sem-stubs
- zero-regressão
- compilar-antes-da-entrega
- testar-com-a-mudança
- evidência-antes-de-inferência
- mantenedores-controlam-arquitetura
- sem-fallback-silencioso
- sem-api-inventada

---

## Papel e autoridade

- **IA acelera a implementação; IA não define arquitetura, produto nem política.**
- Design, prioridades e merge para a branch de produção são decisão dos mantenedores.
- Fluxo de entrega: **feature → `main`** via PR. PR precisa de aprovação de alguém que não empurrou
  o código.
- **Toda mudança relevante tem origem rastreável**: uma issue aberta com o plano de implementação,
  uma tarefa do backlog ou um pedido explícito dos mantenedores.
- **Toda entrega tem prova**: teste executado, verificação de comportamento. Concluído significa
  comprovado, não pretendido.

---

## O que este repositório é

Gateway reverso em Go para os serviços de Cloud/ML do Énóis. Ele aplica controle de admissão antes de
chamar o upstream e mantém o número de chamadas concorrentes limitado.

- **O gateway não é o serviço.** Ele roteia, autentica por chave, limita taxa e repassa. Regra de
  negócio do modelo mora no upstream.
- **`GET /healthz` é respondido pelo próprio gateway** e nunca é proxiado. Quebrar isso derruba o
  health check de todo mundo.
- **Cabeçalho de confiança é do gateway.** `X-Gateway-Client-IP` e `X-Gateway-Route` são escritos por
  ele, substituindo qualquer cópia enviada pelo cliente. Nunca repasse o valor do cliente.
- **Rota sem `api_keys` é pública** e o gateway avisa no boot. Rota pública nova é decisão de
  mantenedor, não de agente.
- **Configuração inválida falha no boot**, não em runtime. Padrão de rota desconhecido, chave de
  rate limit desconhecida e limite inválido são erro de subida.

---

## Estado do projeto — ler antes de trabalhar

1. `git fetch` + `git log` recente: **confira que outro agente não está fazendo a mesma coisa**
   (branches, PRs abertos, commits de hoje).
2. [`README.md`](README.md) — rotas, admissão, erros e configuração.
3. [`CONTRIBUTING.md`](CONTRIBUTING.md) — regras de código e de PR.
4. [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — convivência. Vale para agente também.

---

## Ciclo de trabalho

1. Ler o estado (acima).
2. Escolher a tarefa de maior valor que ninguém reivindicou.
3. Implementar **um escopo completo** — nada de meio-corte, nada de stub "por enquanto".
4. Testar: no mínimo os testes do que mexeu; a suíte inteira quando o escopo pedir.
5. Commit com mensagem **neutra e curta**.
6. Push na sua branch e PR. Nunca termine com trabalho não commitado.
7. Antes de commitar: `git fetch`, reconcilie com o remoto, procure marcadores de conflito
   (`<<<<<<<`), rode os testes de novo na árvore final.

Regras do ciclo:

- **Nunca perguntar quando as evidências do repositório podem responder.**
- **Nunca encerrar um turno com uma unidade não commitada.**
- **Nunca encerrar um turno apenas com um resumo.**
- Exatamente um item do todo pode estar `in_progress`.
- **Concluído significa comprovado, não pretendido.**

Pare e reporte quando a decisão exigir produto (só mantenedor decide), houver colisão inevitável com
a lane de outro agente, ou depender de acesso que você não tem.

---

## Gate de qualidade (Q0–Q7)

Fluxo de correção: **reproduzir → corrigir a causa-raiz → adicionar teste de prova → rodar os testes
relevantes → rodar a suíte → caçar casos-limite → commit → push.**

- **Q0 — causa-raiz.** Bug é corrigido na causa, não no sintoma. O teste de prova **falha no código
  antigo** e passa no novo.
- **Q1 — cobertura.** Toda funcionalidade tem teste; toda correção tem teste de regressão; teste e
  implementação compartilham o commit.
- **Q2 — verde antes do push.** `go build ./...` e `go vet ./...` verdes **antes** de empurrar.
- **Q3 — casos-limite.** Cobrir quando aplicável: rota inexistente, padrão inválido, chave ausente,
  limite zero, corpo vazio, upstream fora do ar, timeout, concorrência no limite do bulkhead.
  **Só o caminho feliz é insuficiente.**
- **Q4 — caça.** Procurar ativamente: entrada extrema, fronteira do contrato de rota, regressão em
  rota vizinha, comportamento não coberto.
- **Q5 — honestidade do verde.** Sem falso verde, sem asserção enfraquecida, sem `t.Skip` escondido.
  Vermelho seu é corrigido ou revertido.
- **Q6 — a suíte é o mínimo, não o teto.**
- **Q7 — proibido.** Stub, placeholder, `TODO`/`FIXME` fingindo conclusão, handler que devolve 200
  vazio para esconder ausência, branch não tratada engolida, suporte fingido como "não
  implementado". Comportamento não suportado responde com erro explícito.

**Regra do escoteiro: toda falha é corrigida quando encontrada.** Teste quebrado ou instável, build
quebrado, erro de lint ou de tipo, bug ou falha de segurança que você encontrar é corrigido na mesma
tarefa, mesmo que seja anterior à sua mudança. Se o conserto estiver realmente fora de alcance,
registre na mesma tarefa e diga na PR — nunca deixe em silêncio.

---

## Regras de segurança (não negociáveis)

- **O repositório é público e há atacante lendo.** Mensagem de commit e descrição de PR são neutras:
  diga *o que* mudou, **nunca detalhe *como* uma falha de segurança foi corrigida** (vetor, payload,
  regra de detecção).
- **Segredo nunca em código, log, commit, PR ou relatório.** Chave de API, token, senha e dado
  pessoal não aparecem em lugar algum — nem em fixture de teste. `gateway.example.yaml` é exemplo;
  `gateway.yaml` real nunca é commitado.
- **Nunca desative um controle de segurança para fazer teste passar** (gatekeeper, rate limit,
  validação de rota). Se o teste pede isso, o teste está errado.
- **Cabeçalho de confiança nunca vem do cliente.** `X-Gateway-Client-IP` e `X-Gateway-Route` são
  sobrescritos pelo gateway. Um caminho que preserve o valor do cliente é vulnerabilidade.
- **Produção é intocável por agente:** nenhuma ação destrutiva, nenhuma escrita, nenhuma conta de
  teste. Validação é sonda de leitura pública (`GET /healthz`).
- **Push na `main` pode disparar deploy.** Não force push, não dispare workflow manual, não "conserte"
  pipeline. Se o deploy falhar, diagnostique pelo log de CI e reporte.

---

## Gate de código

- **Sem comentário narrando o óbvio.** Comentário registra restrição que o código não mostra.
- **Sem código morto, sem import órfão, sem variável não usada, sem `fmt.Println` de depuração.**
- **Sem dependência nova sem justificativa na PR.** A biblioteca padrão vence biblioteca nova.
- **Sem refatoração que a tarefa não pediu.**
- **Erro sempre tratado.** `err` não é descartado com `_` sem motivo escrito; erro de configuração
  falha no boot.
- **Tamanho:** alvo de ≤500 linhas por arquivo. Divida por responsabilidade, nunca por sufixo
  numérico.
- **Idioma:** código, nomes e mensagens em inglês, como o resto do repositório. Documentação de
  governança em português.
- **O compilador e os testes mandam sobre a memória.** Sintaxe incerta? Compile primeiro. Nunca
  invente API, assinatura ou comportamento — verifique no código-fonte.

---

## Verificação

```bash
go build ./...          # compilação
go vet ./...            # análise estática
go test ./...           # a suíte
go test ./internal/gateway -run NomeDoTeste -v   # o teste do que você mexeu
```

- **Nunca afirmar que está verde sem prova executada.**
- **Guarda ambiental não é regressão sua** — mas precisa ser dita na PR, com o motivo e a evidência
  de que o mesmo erro existe na base limpa.
- **Teste não depende de rede.** Upstream externo precisa de servidor de teste local (`httptest`).

---

## Commit e entrega

Antes do commit:

- Q0–Q7 satisfeitos;
- `go build`, `go vet` e `go test` verdes;
- árvore pós-rebase inspecionada (sem `<<<<<<<`, sem arquivo alheio);
- documentação do módulo atualizada no mesmo commit.

O commit:

- é uma unidade coesa e completa — implementação + prova juntas;
- tem mensagem no imperativo com escopo: `feat(routes): ...`;
- o corpo conta **o que estava errado e o que muda**;
- **não** tem `Co-Authored-By`, "Generated with" nem nome de ferramenta.

A PR:

- referencia a issue de origem;
- descreve **como você verificou**: qual teste, quantos passaram, o que foi conferido à mão;
- declara o uso de IA no checklist do template;
- não mistura escopos: uma feature por PR.

---

## Multiagente

- **Nunca empurre trabalho alheio**, nunca force uma branch de outro, nunca dispute a mesma cópia de
  trabalho.
- Compilar/testar em paralelo? Use `git worktree` separado.
- Se o push falhar porque o remoto andou, **reconcilie com o conteúdo atual** (preserve os dois
  lados), nunca sobrescreva. Nunca `checkout --ours` nem `--theirs` para "resolver" conflito.

---

## Documentação

- `README.md` descreve o que **existe** (implementado e verificado). Plano não implementado fica em
  issue.
- Decisão tomada em conversa vira documento no mesmo commit que a implementa.
- Código mudou? A doc do módulo muda junto no mesmo PR.
- Documento que descreve comportamento que o código não tem é bug de documentação — corrija ou
  remova.

---

## Contribuindo com IA

1. **Declare o uso.** Marque a caixa de IA no template da PR. Sem nome de ferramenta e sem linha de
   atribuição no commit.
2. **Quem submete responde pelo código.** Entenda cada linha antes de abrir a PR.
3. **Prova, não promessa.** "Deveria funcionar" não é verificação.
4. **Zero slop.** Sem comentário narrando o óbvio, sem arquivo gerado em massa, sem dependência nova
   sem justificativa, sem refatoração que a tarefa não pediu, sem abstração especulativa.
5. **Segurança vem primeiro.** As regras acima valem integralmente.
6. **Revisão humana antes do merge continua sendo a regra**, com aprovação de quem não empurrou.

---

## Autoverificação final

Antes de dizer que terminou, confira cada item. **Se qualquer um for falso, o estado é `não-pronto`.**

- [ ] `go build`, `go vet` e `go test` verdes
- [ ] causa-raiz corrigida, não o sintoma
- [ ] teste de prova falha no código antigo
- [ ] casos-limite relevantes cobertos
- [ ] sem falso verde, sem asserção enfraquecida, sem skip escondido
- [ ] sem stub, sem placeholder, sem TODO fingindo conclusão
- [ ] sem segredo, sem dado pessoal, sem detalhe de correção de segurança no texto público
- [ ] cabeçalho de confiança continua sendo sobrescrito pelo gateway
- [ ] sem comentário de ruído, sem código morto, sem dependência injustificada
- [ ] documentação atualizada no mesmo commit
- [ ] árvore limpa, sem marcador de conflito, sem trabalho alheio
- [ ] remoto sincronizado

`if_any_false: state: não-pronto — next: corrigir-antes-da-entrega`
