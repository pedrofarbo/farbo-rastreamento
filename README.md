# Plataforma de Rastreamento Veicular

Plataforma própria de rastreamento: recebe os dados dos rastreadores por TCP,
interpreta o protocolo, guarda posições e eventos no PostgreSQL e mostra tudo
em um painel React em tempo real — incluindo envio de comandos ao aparelho,
com corte e liberação de motor.

```
TKSTAR / GT06
     ↓  TCP
Protocol Adapter  ──→  Telemetria normalizada
     ↓
Domínio (tracking, events, commands)
     ↓
PostgreSQL / Redis
     ↓
REST + WebSocket
     ↓
React
```

---

## Sumário

- [Antes de começar: o estado dos protocolos](#antes-de-começar-o-estado-dos-protocolos)
- [Subindo tudo](#subindo-tudo)
- [Primeiro acesso](#primeiro-acesso)
- [Cadastrando o rastreador](#cadastrando-o-rastreador)
  - [Credenciais dos rastreadores](#credenciais-dos-rastreadores)
- [Apontando o TKSTAR para o servidor](#apontando-o-tkstar-para-o-servidor)
- [Descobrindo a variante do seu aparelho](#descobrindo-a-variante-do-seu-aparelho)
- [Simulador](#simulador)
- [Corte de motor: como funciona a trava](#corte-de-motor-como-funciona-a-trava)
- [API](#api)
- [Observabilidade](#observabilidade)
- [Desenvolvimento](#desenvolvimento)
- [Configuração](#configuração)
- [Licença e marca](#licença-e-marca)

---

## Antes de começar: o estado dos protocolos

Não existe "o protocolo TKSTAR". O nome cobre aparelhos de fabricantes
diferentes, com firmwares diferentes. Por isso cada adaptador declara o quanto
está confirmado:

| Adaptador | Confiança | O que faz |
| --- | --- | --- |
| `gt06` | **DOCUMENTED** | Protocolo binário GT06/Concox completo: login, posição, heartbeat, alarmes, comandos com correlação de ACK. Boa parte dos aparelhos 4G com relé fala este protocolo. |
| `h02` | **ASSUMED** | Família H02, nas duas formas: binária (`$`) e texto (`*HQ,...#`). É o que a linha TKSTAR fala — TK905, TK915, TK917, TK920. Layout validado contra uma captura real, não contra documentação do fabricante. |
| `tkstar_v1` / `tkstar_v1_8` | **ASSUMED** | Texto `imei:...;` (família GPS103/TK103). Posição, ACC e alarmes por rótulo. |
| `tkstar_v4` | **UNKNOWN** | Esqueleto de captura. Não produz posição nem aceita comando — existe para registrar o tráfego real e dar lugar ao parser correto. |

> **Nem todo aparelho tem relé.** O TK915, por exemplo, é magnético e a
> bateria: não existe saída de corte nele, e o comando de desligar motor
> simplesmente não faz nada. Corte de motor nessa família é documentado para
> modelos com relé (TK920, TK806) — ou, com suporte `DOCUMENTED`, na linha
> Concox/GT06.

O que está marcado como ASSUMED foi inferido de formatos públicos da família,
**não** confirmado contra o firmware do TK910/TK970. Cada campo nessa condição
carrega um `TODO: VERIFY AGAINST DEVICE PROTOCOL` no código.

Consequência prática: **ligue o aparelho e veja o que ele fala** antes de
confiar em qualquer coisa que não seja o `gt06`. A seção
[Descobrindo a variante](#descobrindo-a-variante-do-seu-aparelho) explica como.
Detalhes em [`docs/PROTOCOLS.md`](docs/PROTOCOLS.md).

---

## Subindo tudo

Requisitos: Docker e Docker Compose.

```bash
cp .env.example .env

# Os segredos vêm vazios: cada instalação gera os seus. Isto preenche os
# quatro que o compose exige com valores sorteados (só os que estão vazios):
for var in POSTGRES_PASSWORD REDIS_PASSWORD JWT_SECRET GRAFANA_PASSWORD; do
  sed -i "s|^$var=\$|$var=$(openssl rand -base64 48 | tr -d '\n')|" .env
done

# Falta o primeiro acesso: ADMIN_EMAIL (o seu e-mail) e ADMIN_PASSWORD (uma
# senha só sua, 10+ caracteres). Ajuste também CORS_ORIGINS e APP_URL.
nano .env

docker compose up -d --build
```

Sem `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `JWT_SECRET` ou `GRAFANA_PASSWORD` o
compose nem começa (não há valor padrão para segredo). Com `APP_ENV=production`
(o padrão) o backend ainda confere os valores **antes** de abrir o banco e as
portas, e recusa com uma mensagem dizendo qual variável trocar:

- valor de exemplo ou padrão conhecido (`admin`, `changeme`, `secret`, os
  antigos `troque-...` do `.env.example` etc. — a lista fica em
  [`backend/internal/config/placeholders.txt`](backend/internal/config/placeholders.txt));
- `JWT_SECRET` com menos de 32 caracteres ou que claramente não foi sorteado
  (poucos caracteres diferentes, repetição, sequência). Isso não mede
  entropia: gere com `openssl rand -base64 48`;
- o mesmo valor em dois segredos (ex.: senha do banco igual ao `JWT_SECRET`);
- `REDIS_PASSWORD` vazio com `REDIS_ENABLED=true`.

O Grafana recusa `GRAFANA_PASSWORD` com menos de 12 caracteres, `admin` ou
valor da mesma lista. Com `APP_ENV=development` (ou `test`) o backend só avisa
no log — nunca use assim num servidor.

Sobe seis serviços:

| Serviço | Porta | Aberta para | Para quê |
| --- | --- | --- | --- |
| `frontend` | 3000 | internet | painel web; leva `/api` e `/ws` ao backend |
| `backend` | **5000** | **internet** | **TCP dos rastreadores** |
| `backend` | 8080 | só a máquina | API direta, `/health`, `/metrics` |
| `postgres` | 5432 | só a máquina | banco |
| `redis` | 6379 | só a máquina | replicação de eventos entre instâncias (com senha) |
| `prometheus` | 9090 | só a máquina | métricas |
| `grafana` | 3001 | só a máquina | painéis (admin / `GRAFANA_PASSWORD`) |

"Só a máquina" é `127.0.0.1`: o Docker publica portas passando por cima do
firewall (UFW), então o que não precisa ser público nem é publicado em outra
interface. Para abrir o Grafana do seu computador, use um túnel:
`ssh -L 3001:localhost:3001 usuario@servidor` e acesse <http://localhost:3001>.

As migrations rodam sozinhas na primeira subida.

> **A porta 5000 precisa estar acessível pela internet** — é por ela que o chip
> do rastreador conecta. Se o servidor estiver atrás de NAT, redirecione a
> porta; se houver firewall, libere TCP de entrada.

Confira que está de pé:

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
docker compose ps   # grafana "healthy" = a senha do .env entra de fato
```

### Trocando os segredos

Troque quando um valor vazar, quando uma pessoa com acesso sair, ou se o seu
`.env` ainda tem algum valor do exemplo antigo (a versão atual do backend nem
sobe com eles). Gere cada valor novo com `openssl rand -base64 48` (ou `32`).

Depois de editar o `.env`, `docker compose up -d` recria só os serviços cuja
configuração mudou.

**`JWT_SECRET`** — troque no `.env` e `docker compose up -d`. Todas as
sessões caem e todo mundo entra de novo: os access tokens antigos param de
valer na hora (assinatura) e os refresh tokens do segredo anterior são
recusados e revogados na subida (o log diz quantos). Com várias instâncias,
todas precisam do mesmo valor. O token do Melhor Envios fica cifrado com uma
chave derivada dele: depois da troca, **Pedidos → Conectar Melhor Envios** de
novo.

**`POSTGRES_PASSWORD`** — o PostgreSQL só lê a variável ao criar o volume;
trocar só o `.env` não muda a senha do banco. Primeiro no banco (o `\password`
pede a senha sem mostrar e não a deixa no histórico), depois no `.env`:

```bash
# POSTGRES_USER e POSTGRES_DB do .env (padrão: tracker)
docker compose exec postgres psql -U tracker -d tracker -c '\password tracker'
# agora POSTGRES_PASSWORD=<a mesma senha> no .env, e:
docker compose up -d
```

**`REDIS_PASSWORD`** — não fica gravada em lugar nenhum: troque no `.env` e
`docker compose up -d` (recria o Redis e o backend).

**`GRAFANA_PASSWORD`** — troque no `.env` e `docker compose up -d`. O
Grafana só lê a variável ao criar o volume; por isso o entrypoint do
contêiner ([`deploy/grafana/entrypoint.sh`](deploy/grafana/entrypoint.sh))
grava a senha no admin a cada subida (`grafana cli admin
reset-admin-password`), e o healthcheck só fica `healthy` quando ela entra.
A senha anterior deixa de valer.

**`ADMIN_PASSWORD`** só vale para criar o primeiro acesso. Para trocar a senha
de quem já existe, use **Esqueci minha senha** (a troca encerra as sessões
dessa pessoa).

Se algum valor do exemplo chegou a rodar num servidor acessível, trate como
vazado: troque-o como acima, confira em **Usuários** se não surgiu
administrador desconhecido e revise a auditoria e os logs do período. O
histórico do git guarda os exemplos antigos, mas eles eram só exemplos — nada
de reescrever o histórico; o que importa é nenhum servidor usá-los.

---

## Primeiro acesso

O usuário administrador é criado na primeira subida a partir de `ADMIN_EMAIL` e
`ADMIN_PASSWORD` — e **só** se o banco estiver sem nenhum usuário. Abra
<http://localhost:3000> e entre com essas credenciais. E-mail ou senha de
exemplo nunca viram administrador, em nenhum `APP_ENV`: a subida falha sem
criar ninguém. Depois do primeiro acesso, `ADMIN_PASSWORD` pode ficar vazio.

Perfis disponíveis:

| Perfil | Pode |
| --- | --- |
| `admin` | tudo: cadastro, clientes e faturas, diagnóstico, comandos |
| `operator` | ver o painel e enviar comandos |
| `viewer` | apenas visualizar |
| `customer` | cliente final: só os próprios veículos, mapa e faturas |

### Painel do cliente

O cliente entra pelo mesmo login e vê um painel próprio: **Mapa**, **Meus
veículos** e **Faturas**. Tudo o que é da central (outros veículos, rastreadores,
eventos da frota, cercas da central, diagnóstico) fica fora do alcance dele — a API
responde 404 para veículo alheio e 403 para as rotas da equipe, e o tempo real
(WebSocket) só entrega a ele as mensagens dos próprios veículos. Do rastreador
ele vê a situação, mas não senha de comando, APN nem anotações internas.

Como funciona, do lado da central (menu **Clientes**, perfil `admin`):

1. **Novo cliente**: só os dados — nome, e-mail, telefone e CPF/CNPJ. Sem
   senha, o cliente recebe um e-mail de boas-vindas com o link para criar a
   própria (vale `CUSTOMER_INVITE_TTL`, 72 h).
2. **Novo veículo**: o único jeito de incluir um veículo, para a central e para
   o cliente — veículo, rastreador e assinatura de uma vez (ver abaixo). Depois
   da instalação, a central vincula o rastreador na linha do veículo.
3. **Faturas**: a mensalidade é gerada sozinha `BILLING_INVOICE_LEAD_DAYS`
   dias antes do vencimento (uma por vencimento, sem duplicar). Na ficha, a
   central informa o link de pagamento e/ou o Pix copia-e-cola, dá baixa quando
   recebe, cancela, ou lança uma cobrança avulsa (troca de equipamento, por
   exemplo).
4. **Atraso**: com fatura em aberto vencida há mais de
   `BILLING_SUSPEND_AFTER_DAYS` dias, o cliente perde o acesso ao mapa e aos
   veículos (a API responde 402) até a baixa — as faturas continuam acessíveis,
   e o rastreamento segue gravando. A baixa devolve o acesso na hora.

### Novo veículo: veículo → rastreador → assinatura

Todo veículo de cliente entra pelo mesmo caminho, em três etapas, e cada um
fica com **o seu rastreador e a sua assinatura** (a assinatura aponta para o
veículo). Não há assinatura solta, vaga livre nem veículo avulso.

1. **Veículo** — apelido, placa, marca, modelo…
2. **Rastreador** — para onde vai o aparelho (o endereço de entrega; sem ele, o
   cliente cadastra aqui mesmo, e o CEP preenche rua, bairro e cidade pela
   ViaCEP) e o valor do equipamento. A **instalação não é cobrada pela
   plataforma**: é combinada e paga direto com um prestador (ver abaixo).
3. **Assinatura** — o plano e o dia de vencimento; a mensalidade começa com o
   pedido e a primeira sai no ciclo normal.

A confirmação grava tudo numa transação (ou sai tudo, ou nada — uma placa
repetida não deixa assinatura órfã) e lança a **fatura do equipamento**
(avulsa), que entra em *Faturas* como qualquer outra (Pix, baixa manual,
estorno).

- **Pelo cliente** — *Meus veículos → Novo veículo*. Os preços vêm do servidor
  (`CATALOG_*`, padrão da landing: equipamento R$ 150) e o plano é o mesmo que
  ele já paga (quem tem o preço especial continua nele; no primeiro veículo,
  o padrão). Com o Pix ligado, a janela de pagamento abre em seguida. Fica
  bloqueado com fatura vencida, com o acesso suspenso e com 3 veículos ainda
  aguardando instalação. Cada veículo aparece com a situação do rastreador e
  da assinatura juntas.
- **Pela central** — ficha do cliente → *Veículos → Novo veículo*: os mesmos
  passos, com valor do equipamento (0 não gera fatura — aparelho próprio,
  cortesia), vencimento, plano (sugere o que o cliente já paga) e, se o
  aparelho já foi instalado, o vínculo na hora (aí não há envio). Sem o limite
  de pendentes. Cada linha da ficha é um veículo: rastreador, assinatura e as
  ações *Editar assinatura* e *Encerrar*. Encerrada, a linha oferece
  *Reativar assinatura* (sem nova cobrança de equipamento) ou *Excluir*; com
  assinatura ativa o veículo não pode ser excluído.

O endereço de entrega fica no cadastro do cliente (ele altera em *Meus
veículos*; a central, na ficha) e é **copiado na assinatura** no pedido: se o
cliente mudar de endereço depois, a ficha continua mostrando para onde cada
rastreador foi enviado. A central pode incluir veículo de cliente sem endereço
(aparelho entregue em mãos).

**Dados anteriores**: a migração `0008` ligou as assinaturas ativas aos
veículos do mesmo cliente, pela ordem de cadastro. Assinatura que sobrou sem
veículo aparece como *Assinatura sem veículo*, com *Informar veículo* (para o
cliente e para a central, sem nova cobrança).

### Pedidos: o chip M2M e o rastreador até o cliente

Todo **Novo veículo** sem aparelho instalado na hora vira um **pedido**, com
duas linhas do tempo que a central avança no menu **Pedidos** (admin e
operador) e que o cliente acompanha em *Meus veículos → Acompanhar pedido*,
com a data de cada etapa:

| Chip M2M | Rastreador |
| --- | --- |
| Chip solicitado no fornecedor | Aguardando chegada do rastreador pelo fornecedor |
| Chip enviado | Rastreador chegou em nossa base |
| Chip chegou em nossa base | Aguardando chegada do chip M2M *(só se o chip ainda não chegou)* |
| Chip separado para configuração | Rastreador em configuração |
| | Rastreador configurado *(vincula o aparelho/IMEI ao veículo)* |
| | Rastreador enviado *(etiqueta do Melhor Envios, com código de rastreio)* |
| | Rastreador em trânsito *(automático)* |
| | Rastreador chegou *(automático; o cliente vê os instaladores)* |

Regras: o rastreador só entra em configuração com o chip separado; marcar como
configurado pede o aparelho; "Enviado" só sai pela compra da etiqueta; depois
de enviado não volta para antes do envio. *Corrigir uma etapa* ajusta um
status lançado errado, e tudo fica no histórico (com quem mudou) e na
auditoria. A fila de **Pedidos** se divide por etapa — com o fornecedor, na
base, em configuração, prontos para envio e a caminho — e cada pedido abre com
a próxima ação.

**Melhor Envios** (`MELHORENVIO_*`): com o rastreador configurado, o admin cota
o frete para o endereço de entrega do pedido, escolhe o serviço e **compra a
etiqueta** (debita o saldo da carteira do Melhor Envios; o destinatário
precisa ter CPF/CNPJ na ficha). O sistema gera e imprime a etiqueta, grava o
código de rastreio e acompanha a entrega sozinho — consulta a API a cada
`MELHORENVIO_SYNC_INTERVAL` e aceita o webhook assinado em
`POST /api/shipping/melhorenvio/webhook` (cadastre no aplicativo). Postado vira
*Em trânsito*, entregue vira *Chegou*, etiqueta cancelada volta para
*Configurado*. A compra é retomável: se cair no meio, repetir continua de onde
parou, sem pagar duas vezes; sem saldo, a etiqueta sai do carrinho.

A **geração da etiqueta no Melhor Envios é assíncrona** ("o envio já está
sendo processado"): se ela não sair em alguns segundos, o pedido fica
*Etiqueta paga, em geração* e o envio é concluído sozinho pela sincronização —
ou na hora, pelo botão *Concluir envio*. Observado no sandbox: o `status` da
etiqueta continua `released` mesmo depois de gerada, e o rastreio não mostra a
geração; por isso a conclusão confere os detalhes da etiqueta
(`GET /api/v2/me/orders/:id`: data e chave de geração, código `self_tracking`).

A integração usa o **OAuth** do Melhor Envios: um aplicativo (Client ID +
Secret) e o botão *Pedidos → Conectar Melhor Envios*, que leva à autorização e
volta. O callback do aplicativo precisa de um domínio (o Melhor Envios não
aceita `localhost`) e é sempre `APP_URL` + `/api/integrations/melhorenvio/callback`:

| Ambiente | Aplicativo no Melhor Envios | Callback |
| --- | --- | --- |
| Desenvolvimento (sandbox) | sandbox.melhorenvio.com.br, `MELHORENVIO_SANDBOX=true` | `https://farbo.localtest.me:5173/api/integrations/melhorenvio/callback` |
| Produção | melhorenvio.com.br, `MELHORENVIO_SANDBOX=false` | `https://painel.farborastreadores.com.br/api/integrations/melhorenvio/callback` |

Em desenvolvimento, `farbo.localtest.me` aponta para `127.0.0.1` e o `npm run
dev` serve em HTTPS: use o mesmo endereço em `MELHORENVIO_REDIRECT_URL`. Em
produção, deixe `MELHORENVIO_REDIRECT_URL` vazio (vale o padrão a partir do
`APP_URL`). O token (30 dias) é renovado sozinho e fica **cifrado** no banco. O
cliente recebe e-mail em dois marcos: rastreador **enviado** (com o código) e
rastreador **chegou** (com o link dos instaladores).

### Prestadores de instalação

A instalação é feita por técnicos parceiros e **paga direto a eles**. A central
cadastra os recomendados no menu **Prestadores** (nome, WhatsApp, cidade,
regiões atendidas, se atende moto e/ou carro, valores de referência — em branco
aparece "a combinar" — e uma descrição curta). Os ativos aparecem:

- na **landing page**, na seção *Instalação profissional*: os cards de moto e
  carro e o botão *Ver prestadores recomendados* abrem a lista, com filtro por
  tipo de veículo, busca por cidade e o botão *Chamar no WhatsApp* (mensagem
  pronta);
- no **painel do cliente**, na contratação de um rastreador e nos veículos
  que aguardam instalação.

*Ocultar* tira o prestador da lista sem apagar o cadastro. A lista pública sai
de `GET /api/public/installers`, sem login e sem os dados de controle.

O cliente pode **bloquear e desbloquear** o motor e pedir posição dos próprios
veículos, com a mesma trava de velocidade da central e registro na auditoria.
Comandos de configuração e texto livre continuam só com a central.

Cancelar uma assinatura cancela as faturas dela que ainda não venceram; as já
vencidas continuam em aberto. O veículo continua com o cliente, mas ele não
consegue cadastrar outro sem uma nova assinatura.

### Pagamento por Pix (AbacatePay)

Com `ABACATEPAY_API_KEY` definida, cada fatura em aberto ganha o botão
**Pagar com Pix**: o QR Code e o copia-e-cola aparecem na própria tela (checkout
transparente, sem redirecionar) e a confirmação é **automática** — a fatura é
quitada (`paidVia = PIX`) e, se o cliente estava suspenso, o acesso volta na
hora. A central também pode gerar o Pix pela ficha do cliente (**Gerar Pix**)
para mandar por outro canal. A baixa manual e o link/Pix informados à mão
continuam disponíveis como "outro meio".

A confirmação chega por três caminhos, e qualquer um basta:

1. **Janela aberta**: enquanto o QR Code está na tela, ela consulta o status a
   cada 4 s.
2. **Webhook** (produção): a AbacatePay avisa em
   `POST /api/payments/abacatepay/webhook`. Cadastre no painel dela (ou em
   `POST /v2/webhooks/create`) o endereço público **HTTPS**
   `https://SEU-DOMINIO/api/payments/abacatepay/webhook?webhookSecret=SEU_SEGREDO`,
   com o mesmo segredo de `ABACATEPAY_WEBHOOK_SECRET` e os eventos
   `transparent.completed`, `transparent.refunded`, `transparent.disputed` e
   `transparent.lost`.
3. **Consulta periódica**: a cada minuto o backend reconsulta os Pix pendentes
   — é o que dá baixa em desenvolvimento, onde a AbacatePay não alcança a sua
   máquina.

O webhook é conferido duas vezes (segredo na URL e assinatura HMAC-SHA256 do
corpo em `X-Webhook-Signature`), eventos repetidos são ignorados pelo `id`, e
mesmo assim o conteúdo não decide nada: o backend reconsulta o Pix na API da
AbacatePay antes de dar baixa. Pix pago para fatura que a central já tinha
quitado ou cancelado fica na auditoria como `PAYMENT_UNMATCHED`, para revisão.

**Estorno**: na ficha do cliente, a seção **Pagamentos Pix** lista o que foi
recebido; **Estornar** (com motivo obrigatório) devolve o valor ao cliente pela
AbacatePay. Ela só faz estorno integral, tira o valor do saldo da conta e
recusa pagamento em disputa. Se foi aquele Pix que quitou a fatura, a fatura
volta a ficar em aberto (cancele-a em seguida se a cobrança não for mais
devida); se era um pagamento a mais — o cliente pagou o Pix e também por outro
meio —, a fatura continua quitada. No sandbox o estorno conclui na hora; em
produção é assíncrono e aparece como "Estorno em andamento" até o webhook
`transparent.refunded` (ou a consulta periódica) confirmar. Estornos feitos
direto no painel da AbacatePay também são refletidos aqui.

**Testes**: com chave `abc_dev_...` os Pix são do sandbox e a janela mostra
**Simular pagamento**, que percorre o fluxo inteiro sem dinheiro de verdade.
Em produção esse botão não aparece (e a AbacatePay recusa a simulação).

O pagador (nome, e-mail, CPF/CNPJ, celular) só é enviado à AbacatePay quando o
CPF/CNPJ do cadastro é válido e há celular — ela recusa o Pix inteiro por um
documento inválido. Sem isso, o Pix sai sem identificação e é pago do mesmo
jeito.

### Esqueci minha senha

Na tela de login, **Esqueci minha senha** manda por e-mail um link para criar
uma senha nova. Para os e-mails saírem, configure o SMTP do seu provedor no
`.env`:

```bash
APP_URL=https://painel.seu-dominio.com.br   # para onde o link do e-mail aponta
SMTP_HOST=smtp.seu-provedor.com
SMTP_PORT=587                                # 465 com SMTP_TLS=tls
SMTP_TLS=starttls
SMTP_USERNAME=...
SMTP_PASSWORD=...
MAIL_FROM="Farbo Rastreadores <nao-responda@seu-dominio.com.br>"
```

O domínio do `MAIL_FROM` precisa estar autorizado no provedor (SPF e DKIM);
sem isso os e-mails caem no spam. Sem `SMTP_HOST` o backend avisa no log ao
subir e nenhum e-mail sai — em `APP_ENV=development` o conteúdo do e-mail
(com o link) vai para o log, para dar para testar sem servidor de e-mail.

Como funciona:

- a resposta é a mesma com ou sem conta para o e-mail informado, para a tela
  não servir para descobrir clientes;
- o link vale por `PASSWORD_RESET_TTL` (padrão 1 hora), uma única vez, e um
  pedido novo invalida o anterior; no banco fica só o hash do token;
- no máximo um e-mail por minuto para a mesma conta, além do limite por IP;
- trocar a senha encerra todas as sessões abertas do usuário e manda um
  e-mail de aviso de que a senha foi alterada;
- pedidos, trocas e links inválidos ficam na auditoria
  (`AUTH_PASSWORD_RESET_*`).

Para ver os e-mails em desenvolvimento sem mandar nada de verdade, suba o
[Mailpit](https://mailpit.axllent.org) (`docker run -p 1025:1025 -p 8025:8025
axllent/mailpit`) e use `SMTP_HOST=localhost SMTP_PORT=1025 SMTP_TLS=none
MAIL_FROM=teste@localhost`; a caixa fica em <http://localhost:8025>.

### Alertas por e-mail

O cliente recebe por e-mail o que importa sobre os veículos dele e escolhe o
que quer receber em **Alertas**, no menu do painel. Na ficha do cliente, a
central vê as mesmas escolhas e o histórico do que foi enviado.

| Alerta | Quando sai | Padrão |
| --- | --- | --- |
| Botão de pânico (SOS) | o aparelho manda o alarme SOS | ligado |
| Bateria do veículo desconectada | alarme de corte de energia | ligado |
| Movimento com a ignição desligada | o veículo se afasta `ALERTS_TOWING_DISTANCE_M` (300 m) de onde estacionou, sem ignição — ou o alarme de deslocamento do GT06 | ligado |
| Ignição no horário de vigilância | ignição ligada dentro do horário escolhido (padrão 22:00–06:00) | ligado |
| Excesso de velocidade | passa do limite cadastrado no veículo | ligado |
| Rastreador sem sinal | parou de comunicar: na hora se estava em movimento; parado, só depois de `ALERTS_OFFLINE_PARKED_AFTER` (2 h) | ligado |
| Bateria do rastreador fraca | alarme de bateria baixa | ligado |
| Bloqueio e desbloqueio do motor | o aparelho confirmou o comando | ligado |
| Ignição ligada (qualquer horário) | toda ignição | desligado |

O que evita e-mail demais:

- **Eventos antigos não viram e-mail.** Quando o rastreador volta do
  sem-sinal, ele descarrega o que guardou. Nada disso gera alerta: só vale o
  que chegou com menos de `ALERTS_MAX_EVENT_AGE` (10 min) de atraso.
- **Intervalo mínimo.** O mesmo alerta, do mesmo veículo, para a mesma
  pessoa, sai no máximo uma vez a cada `ALERTS_COOLDOWN` (30 min). As
  repetições no meio ficam no histórico e aparecem como contagem no e-mail
  seguinte.
- **Teto por hora.** Cada destinatário recebe no máximo `ALERTS_MAX_PER_HOUR`
  (20) alertas por hora.
- **Reboque sem falso alarme.** O ponto de referência é onde o veículo
  estacionou. Ruído do GPS parado não conta: é preciso sair do raio em dois
  pontos seguidos, ou em um com velocidade acima de 10 km/h. Um alerta por
  estacionamento.
- **Suspensão.** Com o acesso suspenso por atraso, o cliente não recebe
  alertas até pagar.

A central recebe os alertas em `ALERTS_CENTRAL_EMAILS`:

- todos os alertas padrão dos veículos dela (sem dono);
- os de segurança (SOS, bateria desconectada e reboque) de todos os veículos,
  inclusive de cliente suspenso ou que desligou esses alertas.

**Cercas do cliente.** No app (**Alertas → Cercas**, ou **Criar cerca aqui**
na tela do veículo), o cliente desenha um círculo em volta de casa, do
trabalho ou da escola e escolhe:

- **Veículos:** só os dele, e ao menos um. A cerca vigia só os escolhidos.
- **Tamanho:** de 50 m a 50 km. Abaixo de 50 m, o erro do GPS faria o veículo
  parado "entrar e sair" sozinho.
- **Avisos:** ao entrar, ao sair ou nos dois casos. Os avisos vão por e-mail
  e para o celular.

Cada conta tem até 20 cercas. O aviso respeita o intervalo mínimo e o teto
por hora, contados por cerca: entrar no trabalho logo depois de sair da
escola não é tratado como repetição.

As entradas e saídas ficam no histórico do veículo mesmo com o aviso
desligado. Algumas situações não geram aviso falso:

- **Cerca nova ou alterada:** quem já estava dentro, pela última posição,
  não "entra".
- **Cerca apagada, ou veículo tirado dela:** não gera "saída".

A cerca do cliente só vale enquanto o veículo for dele. As cercas da central,
criadas pelo admin no painel, continuam valendo para a frota inteira e geram
só eventos, sem aviso ao cliente.

**E-mail de teste.** O botão na tela Alertas manda um e-mail de teste na hora,
para o cliente conferir que está chegando (limite de um por minuto).

**Como funciona por dentro.** Os alertas não atrasam a ingestão:

- quando um evento ou uma posição é gravado, ele só entra numa fila;
- a regra é avaliada e o e-mail é enviado em segundo plano;
- se a fila encher, o alerta é descartado e um aviso vai para o log;
- o histórico (`alert_notifications`) guarda o que saiu, falhou ou foi
  segurado, e é apagado depois de 90 dias.

O e-mail traz o local do evento, com endereço (Nominatim, quando responde) e
link para o mapa, e um botão que abre o veículo no painel.

### App do cliente (PWA)

Além do painel, o cliente tem um app para o celular em **`/app`**
(`https://painel.seu-dominio.com.br/app`). É a mesma conta e a mesma API,
numa interface pensada para o celular:

- **Mapa:** os veículos ao vivo, com a lista embaixo (situação, ignição,
  velocidade, última atualização).
- **Veículo:** endereço, ignição, velocidade, motor, bateria e sinal. Também
  bloqueio e liberação do motor (com a mesma trava do painel), trajeto de
  hoje, de ontem ou das últimas 24 h com distância e velocidade máxima,
  **Como chegar**, **Compartilhar** e os últimos eventos.
- **Mapa em tela cheia:** pelo botão no canto do mapa do veículo. O mapa
  segue o veículo ao vivo até o cliente arrastá-lo; a mira volta a seguir.
  Mostra também o trajeto (hoje, ontem, 24 h). O "voltar" do celular, o X ou
  o Esc fecham sem sair do veículo.
- **Veículos, Faturas e Alertas:** as mesmas telas do painel, com Pix e
  acompanhamento do pedido.
- **Cercas:** dentro de Alertas. O cliente arrasta o mapa por baixo de um
  pino fixo, ou usa **Onde estou** ou o atalho de um dos veículos, e ajusta o
  raio num controle deslizante. As cercas aparecem no mapa e na tela do
  veículo.
- **Conta:** instalar o app, abrir o painel completo e sair.

É um PWA:

- **Instalável:** no Android e no Chrome, pelo botão **Instalar o app** em
  Conta ou pelo menu do navegador; no iPhone, em Compartilhar → Adicionar à
  Tela de Início. Abre pelo ícone, em tela cheia.
- **Offline:** o app fica guardado no aparelho. Sem internet, ele abre com os
  últimos dados dos veículos, faturas e alertas, e avisa que está offline.
  Sair da conta apaga esses dados e desliga as notificações daquele aparelho.
- **Atualização:** quando sai versão nova, o app mostra "Nova versão
  disponível — toque para atualizar".

**Notificações no celular (Web Push):**

- **O que chega:** os mesmos alertas do e-mail chegam como notificação
  (SOS, bateria desconectada, reboque, ignição na vigilância e os demais), com
  as mesmas escolhas e os mesmos filtros (intervalo mínimo e teto por hora).
- **O que o toque faz:** abre o veículo no app.
- **Alertas de segurança:** ficam na tela até o cliente ver.
- **Como ligar:** o cliente liga em **Alertas → Ativar notificações neste
  celular**. No iPhone, só com o app instalado na Tela de Início (iOS 16.4+).
- **Chaves VAPID:** são geradas na primeira subida e guardadas no banco,
  cifradas com uma chave derivada do `JWT_SECRET`. Trocar o `JWT_SECRET` gera
  chaves novas, e os clientes precisam ligar as notificações de novo.
- **Chaves fixas:** para usar as suas, defina `VAPID_PRIVATE_KEY` (base64url,
  32 bytes).
- **Segurança:** o servidor só envia para os serviços de push dos navegadores
  (Google, Mozilla, Apple, Microsoft), para um cliente não conseguir apontá-lo
  para a rede interna.

No celular, o app se comporta como app nativo:
- **Aparência:** barras translúcidas e mapa em tela cheia.
- **Lista de veículos:** desliza sobre o mapa e para em três posições.
- **Atualizar:** puxar a tela para baixo busca os dados de novo.
- **Transições:** abrir um veículo desliza da direita; voltar desliza da esquerda.
- **Janelas:** sobem de baixo.
- **Toque:** campos com 16 px (o iPhone não dá zoom ao tocar) e alvos de toque de 44 px.
- **Abertura do iPhone:** tem tela própria (`public/app/splash/`, gerada por
  `node scripts/pwa-icons.mjs` junto com os ícones).

**Testar no celular (desenvolvimento).** O `npm run dev` serve em HTTPS.
Com o celular no mesmo Wi-Fi, abra `https://<IP-da-máquina>:5173/app/`; o
Vite mostra os endereços ao subir.

- **Sem certificado confiável:** o Vite usa um certificado provisório
  (`@vitejs/plugin-basic-ssl`). O app abre depois do aviso ("Mostrar detalhes
  → visitar este site"). Ao **Adicionar à Tela de Início**, porém, o iPhone
  não baixa o ícone nem as telas de abertura e mostra só a inicial do nome.
- **Com certificado confiável (recomendado para o iPhone):**
  1. Rode `npm run dev:cert` (em `frontend/`). Ele cria em `frontend/.certs`,
     fora do git, uma autoridade de desenvolvimento e o certificado do
     servidor. A autoridade **só vale para endereços de rede local e
     `localhost`**: não serve para interceptar sites da internet.
  2. Leve `frontend/.certs/farbo-dev-ca.crt` ao iPhone e instale o perfil em
     **Ajustes → Geral → VPN e Gerenciamento de Dispositivos**.
  3. Ative a confiança em **Ajustes → Geral → Sobre → Ajustes de Confiança de
     Certificados**.
  4. Reinicie o `npm run dev`; o Vite passa a usar esse certificado.
  5. Apague o ícone antigo e adicione o app de novo à Tela de Início.
  6. Para tirar depois, remova o perfil em VPN e Gerenciamento de
     Dispositivos.

O WebSocket funciona pelo IP porque o proxy do Vite apresenta ao backend a
origem cadastrada (`http://localhost:5173`) quando a página é dele mesmo.

**Limite do modo de desenvolvimento:**
- não há service worker, então não há modo offline nem notificações;
- esses dois recursos precisam do build (`npm run build` + `npx vite preview
  --host`, que usa o mesmo certificado);
- o iPhone só registra service worker com certificado confiável.

---

## Cadastrando o rastreador

A ordem importa: **cadastre o IMEI antes de apontar o aparelho para o
servidor**. Tráfego de IMEI desconhecido é recusado e a sessão é encerrada.

1. Painel → **Rastreadores** → *Novo rastreador*
2. Informe o IMEI (15 dígitos, impresso no aparelho)
3. Deixe o protocolo em branco — o servidor detecta no primeiro pacote e grava
4. **Novo veículo** → dê um nome, uma placa e vincule o rastreador

O campo *senha de comando* só é necessário se o firmware exigir autenticação
nos comandos (alguns pedem `DYD,123456#` em vez de `DYD#`).

### Credenciais dos rastreadores

A senha APN do chip e a senha de comando do aparelho são **só de escrita**:

- nenhuma leitura da API as devolve, para perfil nenhum — nem para o admin.
  No lugar delas o admin recebe `apnPasswordSet` / `commandPasswordSet`; na
  tela de edição o campo fica em branco com *definida — deixe em branco para
  manter*. Senha vazia (ou ausente) no `PATCH` mantém a atual;
  `clearApnPassword` / `clearCommandPassword` apagam;
- o aparelho recebe o comando real, mas o que é gravado no histórico, na
  auditoria e publicado no WebSocket sai com a senha trocada por `***`
  (`DYD,***#`) — inclusive dentro do pacote binário do GT06, nos textos
  personalizados (overrides), no comando livre e na resposta do aparelho,
  que alguns firmwares ecoam. A troca é pelo valor e ignora
  maiúsculas/minúsculas: senha curta demais (ou contida no IMEI) gera `***` a
  mais no histórico — use senhas de 6 caracteres ou mais;
- os overrides saem para o admin com `***` no lugar da senha. Reenviar o
  texto exatamente como veio mantém o original; um texto novo com `***` é
  recusado (use a senha de verdade, ou omita `commandOverrides` no `PATCH`
  para não mexer neles);
- operador e visualizador veem do rastreador só identificação, situação,
  linha e anotações — APN, usuário APN, servidor, intervalos e overrides são
  do admin. **Configuração** (`GET /api/devices/:id/provisioning`) também é só
  do admin, e mesmo ali os comandos sugeridos mostram `***` onde vai a senha
  de comando (`"redacted": true`): quem envia o SMS digita a senha no lugar;
- a auditoria registra *quais* credenciais mudaram em cada cadastro/edição
  (`credentials: ["commandPassword", …]`), nunca os valores.

> **Versões anteriores expunham essas senhas** a qualquer usuário da equipe
> (operador e visualizador inclusive) nas leituras de rastreador, veículo e
> histórico, e a senha de comando ia em claro para o WebSocket. A migration
> `0012` limpa o histórico já gravado (senha atual onde estiver; senha antiga
> na posição conhecida dos comandos GT06), mas não desfaz o que já foi visto:
> **troque a senha de comando dos aparelhos que a usam** (no J16,
> `RESETPWD,<atual>,<nova>#` por SMS — ver [docs/J16.md](docs/J16.md); nos
> demais, conforme o manual) e atualize o cadastro, e troque com a operadora
> a senha APN dos chips que tinham senha cadastrada.

> **Aparelhos H02 (linha TKSTAR) costumam reportar um identificador curto**, de
> 10 dígitos, e não o IMEI de 15. Cadastre exatamente o que o aparelho manda.
> Para descobrir qual é, deixe-o conectar uma vez e rode
> `docker compose logs backend | grep "não está cadastrado"` — o identificador
> aparece ali. Se estiver mascarado, ponha `LOG_MASK_IMEI=false` e repita.

---

## Apontando o TKSTAR para o servidor

A configuração é feita **no aparelho**, por SMS, com o chip já ativo. Os
comandos variam por modelo — confira no manual do seu. Os formatos mais comuns
na linha TKSTAR:

```text
# APN da operadora (exemplo Vivo)
apn123456 zap.vivo.com.br

# Servidor e porta
adminip123456 <IP_DO_SEU_SERVIDOR> 5000

# Intervalo de envio em segundos
upload123456 30

# Conferir o que o aparelho entendeu
check123456
```

`123456` é a senha padrão de fábrica da maioria desses aparelhos — troque-a.

Para os modelos Concox/GT06, o comando de servidor costuma ser:

```text
SERVER,1,<HOST_OU_IP>,5000,0#
```

O painel monta esse texto para você: **Rastreadores → Configuração**. Ele
apenas mostra o comando; **nada é enviado automaticamente** ao aparelho.

Mais exemplos e o roteiro completo em [`docs/TKSTAR.md`](docs/TKSTAR.md).

---

## Descobrindo a variante do seu aparelho

Se o aparelho conectar e nada aparecer no painel, é porque nenhum adaptador
reconheceu o tráfego. O servidor guarda esses bytes em vez de descartá-los.

1. Ligue a captura no `.env`:

   ```env
   TKSTAR_V4_CAPTURE=true
   ```

   ```bash
   docker compose up -d backend
   ```

2. Deixe o aparelho conectar.

3. Painel → **Diagnóstico → Pacotes não interpretados**, ou direto no banco:

   ```sql
   SELECT received_at, remote_addr, payload_ascii, payload_hex
   FROM raw_packets ORDER BY received_at DESC LIMIT 20;
   ```

4. Leia o que apareceu:

   | O payload começa com | Então |
   | --- | --- |
   | `78 78` ou `79 79` | é GT06 — já funciona, veja os logs para o erro real |
   | `24` (`$`) | H02 binário → `h02`, já funciona |
   | `*HQ,` ou `*TK,` | H02 texto → `h02`, já funciona |
   | `imei:` ou `##,imei:` | família GPS103 → `tkstar_v1` |
   | outra coisa | implemente o parser em `protocol_v4.go`, que tem o passo a passo comentado |

5. Antes de colocar em produção, escreva o teste com os **bytes reais**
   capturados. Os arquivos em `internal/protocols/*/` mostram o padrão.

---

## Simulador

Testa o fluxo inteiro — inclusive corte e liberação de motor — sem hardware.

```bash
cd backend

go run ./cmd/tksim \
  --imei 869247061234567 \
  --host localhost \
  --port 5000 \
  --interval 10
```

Cadastre esse IMEI no painel antes, senão a conexão é recusada (que é o
comportamento correto).

Com `--echo` o simulador repete na resposta o comando que recebeu, senha
inclusa, como alguns firmwares fazem — serve para conferir que o histórico e o
tempo real mostram `***` no lugar dela.

O simulador aceita comandos pela entrada padrão enquanto roda:

```text
acc on          liga a ignição
acc off         desliga a ignição
speed 60        define a velocidade
move            põe o veículo em deslocamento
stop            para o veículo
sos             dispara alarme de pânico
status          mostra o estado atual
quit            encerra
```

Roteiro para ver a trava de segurança funcionando:

1. `speed 60` → tente **Desligar motor** no painel → **recusado**, com o motivo
2. `stop` → tente de novo → o comando sai, o simulador responde
   `DYD=Success!` e o painel mostra *Motor bloqueado*
3. **Liberar motor** → volta ao normal

### Teste de carga

`cmd/loadgen` simula muitos rastreadores GT06 (o protocolo dos J16) ao mesmo
tempo — login, posição no intervalo configurado e heartbeat — e, se pedido,
painéis abertos recebendo tudo pelo WebSocket. Serve para dimensionar e
validar o servidor (rode na própria VPS antes de ligar os clientes).

Os IMEIs são `prefixo + sequência` e precisam estar cadastrados. Para 5000
aparelhos de teste com o prefixo `86912`:

```sql
WITH d AS (
    INSERT INTO devices (imei, model, protocol)
    SELECT '86912' || lpad(i::text, 10, '0'), 'J16', 'GT06' FROM generate_series(1, 5000) AS i
    RETURNING id, imei)
INSERT INTO vehicles (name, plate, device_id)
SELECT 'Carga ' || right(imei, 5), 'LD' || right(imei, 5), id FROM d;
```

```bash
cd backend
go run ./cmd/loadgen -host 127.0.0.1 -port 5000 -devices 5000 -imei-prefix 86912 \
  -interval 10s -ramp 60s -duration 10m \
  -api http://127.0.0.1:8080 -ws 2 -email admin@... -password ...
```

A cada 10 s ele imprime uma linha JSON com conectados, envios por segundo,
erros, reconexões e mensagens recebidas pelos painéis. Acompanhe junto o
`/metrics`, o uso de CPU/memória e o atraso das posições no banco
(`received_at - gps_timestamp`). Apague os aparelhos de teste depois
(`DELETE FROM devices WHERE imei LIKE '86912%'`).

---

## Corte de motor: como funciona a trava

O frontend **nunca** aciona o relé. Ele manda uma intenção; quem decide é o
backend, com a última posição conhecida em mãos:

```text
React
  → POST /api/vehicles/:id/commands/engine-cut
  → backend busca a última posição
  → velocidade > ENGINE_CUT_MAX_SPEED_KMH?        → REJECTED
  → posição mais velha que ENGINE_CUT_MAX_POSITION_AGE? → REJECTED
  → não há posição conhecida?                     → REJECTED
  → grava o comando, envia ao aparelho, audita
  → aguarda ACK (COMMAND_ACK_TIMEOUT)
  → WebSocket → React
```

O padrão é conservador: **5 km/h**. Um comando recusado também vira registro no
banco e na auditoria — recusa não é silêncio.

**Posição antiga: o painel e o app pedem uma nova antes.** Com o carro
estacionado, o rastreador manda posição de hora em hora (`TIMER,30,3600#`), e a
última quase sempre passa de `ENGINE_CUT_MAX_POSITION_AGE` (10 min). Para o
corte não ser recusado por isso, o painel e o app do cliente fazem, depois da
confirmação:

1. Consultam `GET /api/vehicles/:id/commands/engine-cut/check`, que avalia a
   regra no backend (com o relógio dele), sem enviar nada.
2. Se a posição for antiga ou não houver nenhuma, mandam **Solicitar posição**
   (`WHERE#`) e voltam a consultar até chegar uma posição nova (até 1 minuto).
3. Só então mandam o corte, que o backend confere de novo.

O cliente acompanha a etapa "Atualizando a posição do veículo" e pode cancelar
enquanto ela não termina. Se a posição não chegar, o corte não é enviado. Isso
depende de o aparelho responder ao `WHERE#` com um pacote de posição, e não só
com texto; confira isso no aparelho real antes de contar com o recurso.

Além disso, o firmware dos aparelhos com relé costuma ter a própria proteção e
só engata o corte quando a velocidade cai. As duas travas somam; nenhuma delas
substitui a outra.

Todo comando — enviado, recusado, confirmado ou expirado — vai para
`audit_logs` com usuário, IP, o texto exato mandado ao aparelho e o resultado.

---

## API

Autenticação por JWT. `POST /api/auth/login` devolve `accessToken` (curto) e
`refreshToken` (longo, rotacionado a cada uso).

```http
POST   /api/auth/login
POST   /api/auth/refresh
POST   /api/auth/logout
GET    /api/auth/me

POST   /api/auth/forgot-password         {email} → 202 sempre (envia o link se houver conta)
POST   /api/auth/reset-password/validate {token} → 204, ou 410 se o link não vale mais
POST   /api/auth/reset-password          {token, password} → 204; 400 senha fraca; 410 link inválido

GET    /api/me/account                   (customer) assinaturas, veículos, suspensão, próxima fatura
GET    /api/me/subscriptions             (customer) cada uma com o vehicleId
POST   /api/me/subscriptions/:id/vehicle (customer) informa o veículo de uma assinatura antiga sem veículo
GET    /api/me/invoices                  (customer)

GET    /api/customers                    (admin) lista com situação financeira
POST   /api/customers                    (admin) cria o cliente (só dados + convite)
GET    /api/customers/:id                (admin) ficha: assinaturas, veículos, faturas
PATCH  /api/customers/:id                (admin) dados e ativo/inativo
POST   /api/customers/:id/invite         (admin) reenvia o e-mail de boas-vindas
POST   /api/customers/:id/subscriptions/:sid/vehicle  (admin) veículo de assinatura antiga sem veículo
POST   /api/customers/:id/vehicles/:vid/subscription  (admin) reativa: nova assinatura para o veículo
POST   /api/customers/:id/invoices       (admin) fatura avulsa
PATCH  /api/subscriptions/:id            (admin) plano e valor
POST   /api/subscriptions/:id/cancel     (admin)
PATCH  /api/invoices/:id                 (admin) link de pagamento e Pix
POST   /api/invoices/:id/pay             (admin) baixa manual
POST   /api/invoices/:id/cancel          (admin)
POST   /api/invoices/:id/pix             (admin) gera o Pix da fatura (AbacatePay)
GET    /api/charges/:id                  (admin) status do Pix, consultado na AbacatePay
POST   /api/charges/:id/simulate         (admin) só com chave de testes
POST   /api/charges/:id/refund           (admin) {reason} estorno integral do Pix pago

POST   /api/me/invoices/:id/pix          (customer) gera ou reaproveita o Pix da própria fatura
GET    /api/me/charges/:id               (customer) status do Pix
POST   /api/me/charges/:id/simulate      (customer) só com chave de testes

POST   /api/payments/abacatepay/webhook  (público; segredo + assinatura HMAC)

GET    /api/catalog                      preços de um rastreador novo (para o cliente, com o plano dele)
PUT    /api/me/address                   (customer) endereço de entrega (obrigatório para contratar)
POST   /api/me/trackers                  (customer) Novo veículo {vehicle}: preços do catálogo; 409 ADDRESS_REQUIRED sem endereço
POST   /api/customers/:id/trackers       (admin) Novo veículo {vehicle, equipmentCents, setupDueDate, plan}
PUT    /api/customers/:id/address        (admin) endereço de entrega do cliente
PUT    /api/customers/:id/history-retention  (admin) {days: 7|14|30|null} prazo do histórico do cliente
PUT    /api/vehicles/:id/history-retention   (admin) {days: 7|14|30|null} exceção do veículo (null segue o cliente)

GET    /api/me/fulfillments              (customer) acompanhamento dos próprios pedidos
GET    /api/fulfillments                 (admin, operator) fila; ?status=all inclui os entregues
GET    /api/fulfillments/:id             (admin, operator)
POST   /api/fulfillments/:id/status      (admin, operator) {track: CHIP|TRACKER, status, note?, deviceId?}
POST   /api/fulfillments/:id/shipping/sync   (admin, operator) consulta o rastreio agora
POST   /api/fulfillments/:id/shipping/quote  (admin) cotação do frete
POST   /api/fulfillments/:id/shipping/label  (admin) {serviceId} compra, gera e imprime a etiqueta
GET    /api/integrations/melhorenvio     (admin) situação da conexão
POST   /api/integrations/melhorenvio/connect     (admin) URL de autorização (OAuth)
POST   /api/integrations/melhorenvio/disconnect  (admin)
GET    /api/integrations/melhorenvio/callback    (público; vale pelo state) volta da autorização
POST   /api/shipping/melhorenvio/webhook         (público; assinatura HMAC com o Secret)

GET    /api/public/installers            prestadores ativos (público, usado pela landing)
GET    /api/installers                   (admin) todos, com ativos e ocultos
POST   /api/installers                   (admin)
PATCH  /api/installers/:id               (admin)
DELETE /api/installers/:id               (admin)

GET    /api/vehicles                     lista com estado do rastreador junto
POST   /api/vehicles                     (admin) só veículo da central (sem cliente)
GET    /api/vehicles/:id
PATCH  /api/vehicles/:id                 (admin)
DELETE /api/vehicles/:id                 (admin) 409 se o veículo tiver assinatura ativa

GET    /api/vehicles/:id/position
GET    /api/vehicles/:id/positions       ?from&to&limit&simplify&raw&after
GET    /api/vehicles/:id/events
GET    /api/vehicles/:id/commands

POST   /api/vehicles/:id/commands/engine-cut       (operator+)
GET    /api/vehicles/:id/commands/engine-cut/check (operator+) a regra do corte agora, sem enviar
POST   /api/vehicles/:id/commands/engine-resume    (operator+)
POST   /api/vehicles/:id/commands/request-position (operator+)
POST   /api/vehicles/:id/commands/request-status   (operator+)
POST   /api/vehicles/:id/commands                  (operator+; CUSTOM é admin)

GET    /api/devices                      (equipe) sem senhas; config. só para o admin
POST   /api/devices                      (admin)
GET    /api/devices/:id                  (equipe)
GET    /api/devices/:id/status           (equipe)
GET    /api/devices/:id/commands         (equipe) histórico, senha como ***
GET    /api/devices/:id/provisioning     (admin) comandos de configuração sugeridos
PATCH  /api/devices/:id                  (admin) senha vazia mantém; clear* apaga
DELETE /api/devices/:id                  (admin)

GET    /api/geofences                    equipe: as da central; cliente: as dele
POST   /api/geofences                    (admin: da central; cliente: dele, com vehicleIds)
PATCH  /api/geofences/:id                (o mesmo dono; outro dono = 404)
DELETE /api/geofences/:id                (o mesmo dono; outro dono = 404)

GET    /api/events
GET    /api/protocols

GET    /api/diagnostics/connections      (admin)
GET    /api/diagnostics/raw-packets      (admin)
GET    /api/diagnostics/audit-logs       (admin)

GET    /ws?token=<accessToken>           tempo real
```

Nenhuma resposta traz a senha APN nem a senha de comando dos rastreadores, e
o `payload`/`response` dos comandos (histórico e WebSocket) vem com a senha
trocada por `***` — ver [Credenciais dos rastreadores](#credenciais-dos-rastreadores).

### Por quanto tempo o histórico é guardado

O trajeto (posições) e os eventos de cada veículo ficam guardados por **7, 14
ou 30 dias**. O prazo herda: o **veículo** pode ter o próprio; senão vale o do
**cliente** (ficha do cliente → *Histórico dos veículos*); senão o padrão da
central, `HISTORY_RETENTION_DAYS` (30). Aparelho sem veículo segue o padrão.

A limpeza roda na subida e a cada `HISTORY_CLEANUP_INTERVAL` (1 h), em lotes
pequenos (5 000 linhas por DELETE, com pausa entre eles) para não disputar o
banco com a ingestão. As consultas de histórico e de eventos também não
passam do prazo, mesmo entre uma limpeza e outra, e a tela do veículo só
oferece os períodos que cabem nele. Comandos e auditoria não entram: são
registros da operação.

É o que mantém o disco sob controle: com 5000 rastreadores a cada 10 s são
~43 milhões de posições por dia. Para o mesmo fim, cada posição guarda só o
necessário: o pacote bruto não é gravado (ligue `POSITIONS_STORE_RAW` para
investigar um modelo novo; pacotes com problema vão sempre para
`raw_packets`) e a tabela tem só os índices que as consultas usam.

### Histórico não devolve tudo

`GET /api/vehicles/:id/positions` nunca despeja milhões de pontos. Acima de
`HISTORY_MAX_POINTS` o banco amostra uniformemente (preservando o primeiro e o
último ponto) e a resposta diz o que aconteceu:

```json
{
  "total": 48213,
  "returned": 5000,
  "sampled": true,
  "sampleStep": 10,
  "simplified": true
}
```

Para exportar o histórico bruto, use a paginação por cursor: `?raw=true` e
depois `?after=<último id>`.

### Eventos do WebSocket

```json
{
  "type": "position.updated",
  "vehicleId": "…",
  "deviceId": "…",
  "timestamp": "2026-09-20T13:45:30Z",
  "data": { "latitude": -23.5, "longitude": -46.6, "speedKmh": 42.5, "acc": true }
}
```

Tipos: `position.updated`, `device.online`, `device.offline`, `device.stale`,
`vehicle.event`, `command.sent`, `command.acknowledged`, `command.failed`,
`engine.status.changed`.

Com várias instâncias, os eventos passam pelo Redis assinados (HMAC com chave
derivada do `JWT_SECRET`, que por isso tem de ser igual em todas) e com prazo
de 2 minutos. Quem recebe descarta mensagem sem assinatura válida e refaz o
`data` no tipo que o backend publica: rumo em texto, coordenada fora da faixa
ou campo desconhecido não chegam ao navegador. O painel confere de novo cada
evento antes de usá-lo e desenha o marcador do mapa nó a nó, sem HTML.

O token de acesso só é aceito na URL (`/ws?token=`) na abertura do WebSocket,
em que o navegador não deixa mandar cabeçalho; as demais rotas exigem
`Authorization: Bearer`.

---

## Observabilidade

| Endpoint | O que é |
| --- | --- |
| `/health` | o processo está vivo |
| `/ready` | o banco responde e quantas sessões existem |
| `/metrics` | métricas Prometheus |

Métricas principais: `tracker_connections`, `tracker_online_devices`,
`tracker_packets_received_total`, `tracker_packets_invalid_total`,
`tracker_positions_received_total`, `tracker_commands_sent_total`,
`tracker_commands_failed_total`, `tracker_command_latency_seconds`.

O Grafana já sobe com a fonte de dados e o painel **Rastreamento — visão
geral** provisionados: <http://localhost:3001>.

Os logs são JSON estruturado. O IMEI aparece mascarado
(`869247******567`) — desligue com `LOG_MASK_IMEI=false` se precisar depurar.
Senha, token e credencial nunca são registrados.

Tracing distribuído é opcional: aponte `OTEL_EXPORTER_OTLP_ENDPOINT` para um
coletor OTLP. Vazio instala um tracer no-op e o código instrumentado não muda.

---

## Desenvolvimento

Requisitos: Go 1.27 e Node 22.

```bash
# Banco e cache apenas
docker compose up -d postgres redis

# Backend. Sem APP_ENV vale development: segredo de exemplo só gera aviso no
# log (JWT_SECRET ainda precisa de 32+ caracteres: openssl rand -base64 48).
cd backend
export POSTGRES_PASSWORD=... JWT_SECRET=... ADMIN_EMAIL=... ADMIN_PASSWORD=...
export REDIS_ENABLED=true REDIS_PASSWORD=...   # a mesma do .env
go run ./cmd/server

# Frontend (proxy para :8080 já configurado)
cd frontend
npm install
npm run dev
```

Testes:

```bash
cd backend
go test ./...          # parsers, enquadramento TCP, regra de corte
go vet ./...

# Com um Postgres descartável, roda também o teste de ponta a ponta das
# credenciais (API + WebSocket + migration); ele cria e apaga um schema próprio.
FARBO_TEST_DATABASE_URL='postgres://usuario:senha@localhost:5432/banco?sslmode=disable' \
  go test ./internal/api/ -run 'TestCredentials|TestMigration'

cd frontend
npm test               # validação dos eventos do WebSocket, marcador do mapa
```

A suíte cobre o que costuma quebrar em produção: pacote partido entre leituras,
vários pacotes numa leitura só, CRC inválido, protocolo desconhecido, IMEI
desconhecido, correlação de ACK e cada ramo da trava do corte de motor.

---

## Configuração

Todas as variáveis estão comentadas em [`.env.example`](.env.example). As que
mais importam:

| Variável | Padrão | O que muda |
| --- | --- | --- |
| `TCP_PORT` | `5000` | porta dos rastreadores |
| `TCP_IDENTIFY_TIMEOUT` | `30s` | conexão que não faz login nesse prazo é derrubada |
| `TCP_MAX_PENDING_PER_IP` | `100` | conexões sem login aceitas de um mesmo IP |
| `PUSH_ENABLED` | `true` | notificações no celular do app do cliente |
| `VAPID_PRIVATE_KEY` | vazio | chave VAPID fixa (base64url, 32 bytes); vazio gera e guarda no banco |
| `VAPID_SUBJECT` | `mailto:` do `MAIL_FROM` | contato da central para os serviços de push |
| `ALERTS_ENABLED` | `true` | liga os alertas por e-mail |
| `ALERTS_COOLDOWN` | `30m` | intervalo mínimo entre dois e-mails iguais (mesmo alerta, veículo e destinatário) |
| `ALERTS_MAX_EVENT_AGE` | `10m` | evento mais velho que isso ao chegar não vira e-mail |
| `ALERTS_MAX_PER_HOUR` | `20` | teto de alertas por destinatário por hora |
| `ALERTS_OFFLINE_PARKED_AFTER` | `2h` | rastreador parado sem sinal vira alerta depois disso |
| `ALERTS_TOWING_DISTANCE_M` | `300` | deslocamento sem ignição que conta como reboque |
| `ALERTS_CENTRAL_EMAILS` | vazio | e-mails da central: veículos sem dono e alertas de segurança de todos |
| `ALERTS_TIMEZONE` | `BILLING_TIMEZONE` | fuso do horário de vigilância |
| `TRUSTED_PROXIES` | redes privadas | de quem o `X-Forwarded-For` é aceito; com proxy HTTPS na frente, ponha o IP dele |
| `APP_ENV` | `production` no compose, `development` fora dele | fora de `development`/`test`, segredo de exemplo, óbvio ou repetido impede a subida |
| `JWT_SECRET` | — | obrigatório; 32+ caracteres sorteados (`openssl rand -base64 48`); trocar encerra todas as sessões |
| `POSTGRES_PASSWORD` | — | obrigatória; trocar exige `\password` no banco ([Trocando os segredos](#trocando-os-segredos)) |
| `REDIS_PASSWORD` | — | obrigatória no compose e com `REDIS_ENABLED=true` |
| `GRAFANA_PASSWORD` | — | obrigatória no compose; 12+ caracteres, aplicada a cada subida do Grafana |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | vazio | primeiro acesso, só com o banco sem usuários; exemplos são recusados |
| `ENGINE_CUT_MAX_SPEED_KMH` | `5` | acima disso o corte é recusado |
| `ENGINE_CUT_MAX_POSITION_AGE` | `10m` | posição mais velha recusa o corte |
| `COMMAND_ACK_TIMEOUT` | `15s` | sem resposta, o comando vira `TIMEOUT` |
| `DEVICE_STALE_AFTER` | `2m` | sem pacotes, vira `STALE` |
| `DEVICE_OFFLINE_AFTER` | `5m` | sem pacotes, vira `OFFLINE` |
| `HISTORY_MAX_POINTS` | `5000` | teto de pontos por consulta |
| `HISTORY_RETENTION_DAYS` | `30` | padrão da central para guardar o histórico (7, 14 ou 30); cliente e veículo podem ter o próprio |
| `HISTORY_CLEANUP_INTERVAL` | `1h` | intervalo da limpeza do histórico vencido |
| `POSITIONS_STORE_RAW` | `false` | guardar o pacote bruto em cada posição (só para investigação) |
| `DEFAULT_SPEED_LIMIT_KMH` | `0` | `0` desliga o alerta global |
| `GT06_ACK_GPS` | `false` | responder também aos pacotes de posição |
| `H02_BINARY_FRAME_LENGTH` | `0` | tamanho do quadro binário H02; `0` usa heurística |
| `TKSTAR_V4_CAPTURE` | `false` | capturar tráfego não identificado |
| `LOG_MASK_IMEI` | `true` | mascarar IMEI nos logs |
| `APP_URL` | 1ª de `CORS_ORIGINS` | endereço do painel usado nos links dos e-mails |
| `SMTP_HOST` | vazio | servidor de e-mail; vazio desliga o envio |
| `SMTP_PORT` / `SMTP_TLS` | `587` / `starttls` | `465` / `tls` também funciona |
| `MAIL_FROM` | vazio | remetente; obrigatório com `SMTP_HOST` |
| `PASSWORD_RESET_TTL` | `1h` | validade do link de redefinição de senha |
| `CUSTOMER_INVITE_TTL` | `72h` | validade do convite do cliente novo |
| `BILLING_INVOICE_LEAD_DAYS` | `10` | antecedência com que a fatura mensal é gerada |
| `BILLING_SUSPEND_AFTER_DAYS` | `10` | dias de atraso até suspender o acesso do cliente; `0` desliga |
| `BILLING_TIMEZONE` | `America/Sao_Paulo` | fuso do "hoje" dos vencimentos |
| `ABACATEPAY_API_KEY` | vazio | liga o Pix das faturas; `abc_dev_...` é sandbox |
| `ABACATEPAY_WEBHOOK_SECRET` | vazio | segredo do webhook; vazio recusa webhooks |
| `PIX_EXPIRES_IN` | `24h` | validade de cada Pix gerado |
| `CATALOG_*` | preços da landing | plano, equipamento e prazo de um rastreador novo (instalação é paga ao prestador) |
| `MELHORENVIO_CLIENT_ID` / `_CLIENT_SECRET` | vazio | aplicativo do Melhor Envios; liga etiqueta e rastreio |
| `MELHORENVIO_SANDBOX` | `true` | sandbox ou produção do Melhor Envios |
| `MELHORENVIO_FROM_*` | vazio | remetente das etiquetas (a base da central) |
| `MELHORENVIO_SYNC_INTERVAL` | `15m` | intervalo da consulta de rastreio |

---

## Documentação

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — como as peças se encaixam
- [`docs/PROTOCOLS.md`](docs/PROTOCOLS.md) — o que está confirmado e como
  implementar uma variante nova
- [`docs/TKSTAR.md`](docs/TKSTAR.md) — configuração do aparelho, passo a passo
- [`docs/J16.md`](docs/J16.md) — configuração completa do rastreador J16 (GT06
  desbloqueado): comandos SMS, senha padrão, fiação e integração com os
  `commandOverrides` deste projeto

---

## Licença e marca

O código é aberto, sob a [Apache License 2.0](LICENSE): pode ser usado,
modificado e redistribuído, inclusive comercialmente, mantendo o `LICENSE` e o
[`NOTICE`](NOTICE).

A marca não faz parte da licença. Os nomes **FARBO RASTREADORES** e **FARBO
RASTREAMENTO**, os logotipos, as imagens de `frontend/public/assets/` e a
identidade visual (paleta de cores e aparência) são de uso exclusivo da Farbo
Rastreadores. Quem reutilizar o código precisa trocar nome, logotipos e cores
antes de publicar o produto. O que é livre, o que é reservado e onde fica cada
item no código estão em [`MARCA.md`](MARCA.md).
