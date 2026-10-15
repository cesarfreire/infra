# infra

[English](../README.md) | **Português**

Interface de terminal para operar infraestrutura na AWS sem sair do terminal. Liste instâncias, containers, load balancers, tabelas, caches e filas; abra shells e túneis; acompanhe gráficos do CloudWatch e deploys do ECS. Tudo pelo teclado, com suporte a cliques do mouse.

```
 infra   profile: prod ▾   region: sa-east-1 ▾  account 123456789012
╭──────────────────────╮╭──────────────────────────────────────────────────────────────────────────────────╮
│ RESOURCES            ││ Instances 4 found                                                                │
│                      ││ press / to filter                                                                │
│  Favorites           ││  Name                          ID                   State          Type          │
│  Instances           ││ ──────────────────────────────────────────────────────────────────────────────── │
│  Containers          ││  api-prod-1                    i-0a1b2c3d4e5f60718  running        m7g.large     │
│  Load balancers      ││  api-prod-2                    i-0b2c3d4e5f6071829  running        m7g.large     │
│  Key-value DBs       ││  ★ bastion                     i-0c3d4e5f607182930  running        t4g.micro     │
│  Caches              ││  worker-batch                  i-0d4e5f60718293041  stopped        c7g.xlarge    │
│  Queues              ││                                                                                  │
│  Sessions            ││                                                                                  │
╰──────────────────────╯╰──────────────────────────────────────────────────────────────────────────────────╯
 Loaded 4 instances
 enter actions • c connect • p port forward • o console • d details • * favorite • r refresh • / filter …
```

## Funcionalidades

- **A aplicação nunca sai da tela.** Shells SSM, ECS Exec e túneis rodam num pseudo-terminal desenhado dentro do painel da direita. Eles continuam rodando em segundo plano na seção **Sessions**, e `ctrl+]` desanexa o terminal.
- **Perfis e regiões.** Os perfis vêm de `~/.aws/config` e `~/.aws/credentials`. `ctrl+p` troca o perfil e `ctrl+r` troca a região. As credenciais são verificadas a cada troca. Se a sessão SSO expirou, `l` roda `aws sso login` dentro da aplicação.
- **Alertas de permissão.** Falta de permissão IAM ou credenciais inválidas abrem um alerta com a causa e o que fazer.
- **Filtro em todas as listas.** `/` filtra por texto livre ou por campo (`state:running az:1a tag:env=prod`). A busca ignora maiúsculas e acentos.
- **Monitoramento.** A tela de detalhes de cada recurso tem gráficos do CloudWatch com janelas de 1h a 7d, atualizados a cada minuto.
- **Favoritos e recentes.** `*` marca qualquer recurso. A seção **Favorites** leva direto a ele, com listas separadas por perfil e região.
- **Mudanças seguras.** Toda ação que altera um recurso pede confirmação (`y`). Perfis podem ser marcados como somente leitura.
- **Aviso de atualização.** Ao abrir, a aplicação consulta o GitHub em segundo plano, no máximo uma vez por dia, para ver se há uma versão nova. Se houver, o cabeçalho mostra `↑ vX.Y.Z available`; clicar abre a página da release.

| Seção | O que faz |
|---|---|
| **Instances** (EC2) | Shell via SSM, port forward, start/stop/reboot, detalhes com CPU, rede e créditos de CPU |
| **Containers** (ECS) | Clusters › services › tasks. Shell com ECS Exec, port forward para containers, forçar deploy, alterar o desired count, acompanhar rollouts e eventos do service, gráficos de CPU e memória |
| **Load balancers** (ALB/NLB) | Load balancers › target groups › targets com a saúde de cada um. Listeners, requisições, latência e erros 5XX |
| **Key-value DBs** (DynamoDB) | Scan paginado, busca por chave, item em JSON, edição e exclusão de itens, capacidade consumida e throttles |
| **Caches** (ElastiCache) | Redis, Valkey e memcached, incluindo serverless. `valkey-cli`/`redis-cli` e port forward através de um jump host. CPU, memória, conexões e taxa de acerto |
| **Queues** (SQS) | Backlog, DLQ, espiar mensagens, idade da mensagem mais antiga e vazão |
| **Sessions** | Shells e túneis abertos, com o terminal embutido |

## Requisitos

- **Linux ou macOS.** No Windows, use pelo WSL, porque as sessões precisam de um pseudo-terminal Unix.
- **[AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)** e **[Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)** no `PATH`, para shells e túneis. O restante usa o SDK diretamente.
- Credenciais configuradas como no AWS CLI (perfis, SSO, variáveis de ambiente).

## Instalação

**Binário pronto:** baixe o arquivo do seu sistema na página de *Releases* do repositório (`infra_<versão>_<linux|darwin>_<amd64|arm64>.tar.gz`), extraia e coloque `infra` no `PATH`.

**A partir do código:** requer Go 1.27.2. Com `GOTOOLCHAIN=auto` (o padrão), o Go baixa essa versão sozinho.

```sh
git clone <url-do-repositório> infra && cd infra
make build          # gera bin/infra
./bin/infra --version
```

## Uso

```sh
infra                                   # abre no último perfil/região usados
infra --profile prod --region sa-east-1
infra --read-only                       # bloqueia qualquer alteração nesta execução
```

| Flag | Descrição |
|---|---|
| `--profile` | Perfil inicial. O padrão é o último usado, depois `$AWS_PROFILE` |
| `--region` | Região inicial. O padrão é a última usada, depois a do perfil. Sem nenhuma, a aplicação pergunta |
| `--read-only` | Bloqueia toda alteração, independentemente do `settings.json` |
| `--ssm-shell` | Comando do shell em instâncias (documento `AWS-StartInteractiveCommand`). O padrão usa bash se existir, senão sh. Com `""`, usa o documento SSM padrão |
| `--ecs-shell` | Comando do ECS Exec nos containers. O padrão prefere bash |
| `--no-update-check` | Não verifica se há uma versão nova |
| `--version` | Mostra a versão |

### Teclas

**Globais**

| Tecla | Ação |
|---|---|
| `↑`/`↓` ou `k`/`j` | Navegar |
| `enter` / `→` | Abrir a seção ou o item |
| `esc` | Voltar (no topo de uma seção, volta ao menu) |
| `/` | Filtrar a lista (`enter` mantém o filtro, `esc` limpa) |
| `*` | Marcar ou desmarcar como favorito |
| `r` | Recarregar |
| `ctrl+p` / `ctrl+r` | Trocar perfil / região (também clicando no cabeçalho) |
| `q` / `ctrl+c` | Sair (fecha todas as sessões) |

**Por seção**

| Seção | Teclas |
|---|---|
| Instances | `enter` ações · `c` shell · `p` port forward · `o` console · `d` detalhes · `S` start · `X` stop · `B` reboot |
| Containers | clusters: `enter` services · `t` todas as tasks — services: `enter` tasks · `d` detalhes e deploys · `D` forçar deploy · `N` desired count — tasks: `enter` ações · `c` shell · `p` port forward |
| Load balancers | `enter` target groups › targets · `d` detalhes com listeners |
| Key-value DBs | `enter` itens · `f` buscar por chave — itens: `enter` ver JSON · `n` próxima página · `e` editar (`ctrl+s` salva) · `x` excluir |
| Caches | `c` CLI via jump host · `p` port forward via jump host · `d` detalhes |
| Queues | `enter`/`d` detalhes · `m` espiar mensagens |
| Favorites | `enter` ir até o recurso · `x` remover |
| Sessions | `enter` anexar · `x` encerrar · `ctrl+]` desanexar |
| Detalhes | `↑`/`↓` rolar · `t` janela de tempo · `r` atualizar os gráficos |

Em diálogos de confirmação, só `y` confirma. `n`, `esc` ou um clique cancelam.

## Configuração

Os arquivos ficam em `~/.config/infra/` (ou no diretório de configuração do sistema).

- **`settings.json`** é editado por você. Para bloquear alterações em perfis de produção:

  ```json
  {
    "readOnlyProfiles": ["prod", "*-prd"]
  }
  ```

  Adicione `"updateCheck": false` para desligar a verificação de novas versões. Os padrões seguem a sintaxe de `path.Match`. Perfis somente leitura exibem `read-only` no cabeçalho e bloqueiam start/stop, deploys, edição e exclusão de itens, e a leitura de mensagens SQS (que aumenta o contador de recebimentos).

- **`state.json`** é gravado pela aplicação: último perfil e região, janela de tempo dos gráficos, resultado da última verificação de atualização, jump host escolhido para cada cache, favoritos e recentes (por perfil e região). Pode ser apagado a qualquer momento.

## Pré-requisitos na AWS

- **Shell e port forward em instâncias:** as instâncias precisam do SSM Agent e de um instance profile com `AmazonSSMManagedInstanceCore`.
- **ECS Exec:** o service precisa de `enableExecuteCommand: true` (vale para tasks iniciadas depois de ativar) e a task role precisa das permissões `ssmmessages:*`.
- **Métricas de task no ECS:** exigem Container Insights com *enhanced observability* no cluster. As métricas de service funcionam sempre.
- **Caches:** são acessados por um *jump host*, uma instância em execução com o SSM Agent e acesso de rede ao cache. Para o `c` funcionar, ela precisa ter `valkey-cli` ou `redis-cli` instalado.

### Permissões IAM

A aplicação mostra um alerta quando falta uma permissão, então só é preciso conceder as das funcionalidades que você usa.

| Funcionalidade | Ações |
|---|---|
| Leitura | `ec2:DescribeInstances`, `ecs:List*`, `ecs:Describe*`, `elasticloadbalancing:Describe*`, `dynamodb:ListTables`, `dynamodb:DescribeTable`, `dynamodb:Scan`, `dynamodb:Query`, `elasticache:Describe*`, `sqs:ListQueues`, `sqs:GetQueueAttributes`, `cloudwatch:GetMetricData` |
| Shells e túneis | `ssm:StartSession` (documentos `AWS-StartInteractiveCommand`, `AWS-StartPortForwardingSession` e `AWS-StartPortForwardingSessionToRemoteHost`), `ssm:TerminateSession`, `ecs:ExecuteCommand` |
| Alterações | `ec2:StartInstances`, `ec2:StopInstances`, `ec2:RebootInstances`, `ecs:UpdateService`, `dynamodb:PutItem`, `dynamodb:DeleteItem`, `sqs:ReceiveMessage` |

## Desenvolvimento

```sh
make test       # go test -race ./...
make lint       # golangci-lint (configuração em .golangci.yml)
make fmt        # gofmt + goimports
make snapshot   # gera todos os pacotes de release em dist/, sem publicar
go test ./internal/tui/sections/instances -run TestPortForwardForm   # um teste
```

O golangci-lint precisa ser compilado com Go ≥ 1.27.2:

```sh
GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

O projeto segue arquitetura limpa, com as dependências apontando para dentro:

```
cmd/infra                 composition root: monta adaptadores, serviços e seções por perfil/região
internal/domain           entidades e erros, sem SDK nem UI
internal/app              casos de uso e as portas (interfaces) de que dependem
internal/adapter/aws      implementações das portas com o AWS SDK v2 e o AWS CLI
internal/session          processos em pseudo-terminal com emulador VT
internal/prefs            state.json e settings.json
internal/filter           linguagem de filtro das listas
internal/tui              tela principal, seções e componentes (Bubble Tea v2)
```

Os testes não chamam a AWS: cada adaptador recebe uma interface do cliente do SDK, e cada seção depende de interfaces que os testes substituem por fakes. O [`CLAUDE.md`](../CLAUDE.md) descreve a arquitetura em detalhe, incluindo o contrato entre a tela principal e as seções, como adicionar um novo tipo de recurso e as armadilhas conhecidas das bibliotecas.

### Release

A CI (`.github/workflows/ci.yml`) roda testes, lint e `goreleaser check` em cada push e pull request. Para publicar uma versão, crie e envie uma tag; o workflow `release.yml` gera os binários de Linux e macOS (amd64 e arm64) e a release no GitHub. Os builds de release embutem o nome do repositório (`GITHUB_REPOSITORY`), o que ativa a verificação de atualizações; builds locais não fazem essa verificação.

```sh
git tag v0.1.0
git push origin v0.1.0
```
