import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';

import { HarnessDiagram, RelayDiagram } from '@/components/install/J16Wiring';
import { WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';
import { formatMoney, formatPercentBps } from '@/services/format';
import type { PartnerReport } from '@/types';

import { PasswordGate } from './partner/PasswordGate';
import { usePartnerReport } from './partner/usePartnerReport';
import guide from './InstallGuide.module.css';
import styles from './Partner.module.css';
import base from './PreLaunch.module.css';

const HELP_MESSAGE = 'Olá! Sou representante da Farbo e tenho uma dúvida sobre a minha página.';

/** O valor sem o espaço que não quebra ("R$ 6,00"). */
const money = (cents: number) => formatMoney(cents).replace(/\u00a0/g, ' ');

/** Quanto ele ganha, como na página dele. */
function commissionText(r: PartnerReport): string {
  if (r.commissionType === 'percent') {
    const tax = r.commissionTaxBps ? ` (líquida dos impostos da Farbo, de ${formatPercentBps(r.commissionTaxBps)})` : '';
    return `${formatPercentBps(r.commissionBps)} da mensalidade${tax} de cada veículo ativo`;
  }
  return `${money(r.commissionCents)} por mês por veículo ativo`;
}

/** As partes do tutorial que valem para ele (os kits, as instalações, o recebedor…). */
function partsOf(r: PartnerReport) {
  const installs = Boolean(r.manageInstalls);
  const kits = Boolean(r.kits);
  return {
    installs,
    kits,
    split: Boolean(r.recipient?.enabled),
    directPrices: r.installPrices?.mode === 'direct',
    // Quem põe o rastreador no veículo: o passo a passo da ligação.
    installer: installs || kits || Boolean(r.handDelivery),
  };
}

/**
 * O tutorial do representante (/parceiro/<link secreto>/tutorial): como
 * divulgar o link, o que o cliente faz, como acompanhar e receber a comissão
 * e — para quem tem — os kits, o agendamento, o link do instalador, a
 * ligação do rastreador no veículo e os valores de instalação; por fim, o
 * app do cliente (para ensinar quem contratou) e as dúvidas comuns. Mostra
 * só o que vale para ele, com a mesma senha da página.
 */
export function PartnerTutorialPage() {
  const { token = '' } = useParams();
  const { report, error, locked, unlock } = usePartnerReport(token);

  useEffect(() => {
    document.title = 'Como usar · Farbo Rastreadores';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  return (
    <main className={`${base.page} ${guide.page}`} data-theme="dark">
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={base.badge}>Tutorial</span>
      </header>

      {error ? (
        <p className={base.error} role="alert">
          {error}
        </p>
      ) : locked ? (
        <PasswordGate token={token} onUnlocked={unlock} />
      ) : !report ? (
        <p className={base.hint} role="status">
          Carregando…
        </p>
      ) : (
        <Tutorial report={report} token={token} />
      )}
    </main>
  );
}

function Tutorial({ report, token }: { report: PartnerReport; token: string }) {
  const p = partsOf(report);
  const back = (
    <Link to={`/parceiro/${token}`} className={styles.back}>
      ← Voltar para a sua página
    </Link>
  );
  const steps = [
    { id: 'como-funciona', label: 'Como funciona' },
    { id: 'divulgar', label: 'Divulgar o link' },
    { id: 'cliente', label: 'O que o cliente faz' },
    { id: 'acompanhar', label: 'Acompanhar' },
    { id: 'receber', label: 'Receber a comissão' },
    ...(p.kits ? [{ id: 'kits', label: 'Seus kits' }] : []),
    ...(p.installs
      ? [
          { id: 'agendar', label: 'Agendar' },
          { id: 'link-instalador', label: 'No dia da instalação' },
          { id: 'ativar', label: 'Ativar o rastreador' },
        ]
      : []),
    ...(p.installer ? [{ id: 'instalar', label: 'Ligar o rastreador' }] : []),
    ...(p.directPrices ? [{ id: 'valores', label: 'Seus valores' }] : []),
    { id: 'app', label: 'O app do cliente' },
    { id: 'duvidas', label: 'Dúvidas' },
  ];
  const n = (id: string) => steps.findIndex((s) => s.id === id) + 1;

  return (
    <>
      {back}
      <section className={guide.hero}>
        <div>
          <h1 className={guide.title}>Como usar a sua página</h1>
          <p className={guide.lead}>
            {report.name.split(' ')[0]}, este é o passo a passo para divulgar,{' '}
            {p.installs ? 'agendar e instalar, ' : p.installer ? 'instalar, ' : ''}acompanhar os seus clientes e receber a
            comissão. Volte aqui sempre que precisar.
          </p>
        </div>
      </section>

      <nav className={guide.index} aria-label="Etapas do tutorial">
        {steps.map((s, i) => (
          <a key={s.id} href={`#${s.id}`}>
            <span>{i + 1}</span>
            {s.label}
          </a>
        ))}
      </nav>

      <Step id="como-funciona" n={n('como-funciona')} title="Como funciona">
        <ol className={guide.numbered}>
          <li>Você divulga o seu link de indicação (ou o QR Code dele).</li>
          <li>Quem abre o link e contrata vira seu cliente: a contratação fica marcada com o seu código.</li>
          {p.kits && <li>A Farbo manda kits de rastreadores para você: eles ficam no seu estoque.</li>}
          {p.installs && (
            <li>
              Você agenda, ativa o rastreador (se ele nunca conectou) e instala no veículo do cliente, com o link do
              instalador.
            </li>
          )}
          <li>
            Você ganha <strong>{commissionText(report)}</strong> dos seus clientes, em cada mês em que a mensalidade é
            paga.
          </li>
        </ol>
        {!report.active && (
          <p className={base.error}>Seu link está pausado agora: cadastros novos não contam. Fale com a Farbo.</p>
        )}
      </Step>

      <Step id="divulgar" n={n('divulgar')} title="Divulgar o seu link">
        <ul className={guide.checklist}>
          <li>
            Na sua página, em <strong>Seu link de indicação</strong>, toque em <em>Copiar link</em> ou{' '}
            <em>Compartilhar</em> e mande no WhatsApp, no Instagram, no status ou onde você tiver contato com quem tem
            veículo.
          </li>
          <li>
            Para material impresso, use o <strong>QR Code</strong>: quem aponta a câmera do celular cai direto no seu
            link. Em <em>Criar flyer pronto</em>, a arte já vem com a oferta e o seu QR, em A5 para a gráfica, stories ou
            post.
          </li>
          <li>
            Vai fazer a sua própria arte? Baixe só o QR (o SVG é o melhor para gráfica) e deixe com pelo menos{' '}
            <strong>2,5 cm de lado</strong>, com a borda branca em volta.
          </li>
          <li>Os preços do flyer e do site vêm sempre atualizados da Farbo: não anote preço na mão.</li>
        </ul>
      </Step>

      <Step id="cliente" n={n('cliente')} title="O que o cliente faz">
        <ol className={guide.numbered}>
          <li>
            Abre o seu link. O celular dele guarda o seu código por <strong>60 dias</strong>: se ele voltar ao site nesse
            tempo, pelo mesmo aparelho, a contratação ainda conta para você.
          </li>
          <li>Escolhe o plano, informa os dados dele e do veículo e paga a primeira mensalidade.</li>
          <li>
            {report.handDelivery
              ? 'Você entrega o rastreador em mãos, com os seus kits: quem contrata pelo seu link não paga frete.'
              : 'Recebe o rastreador e o acesso ao app.'}
          </li>
          {p.installs && <li>Aparece na sua página, em “Esperando a instalação”, para você agendar.</li>}
        </ol>
        <p className={guide.muted}>
          Se o cliente abrir o link de outro representante depois do seu, vale o último link aberto. Peça para ele
          contratar pelo seu link, de preferência na hora.
        </p>
      </Step>

      <Step id="acompanhar" n={n('acompanhar')} title="Acompanhar os números">
        <ul className={guide.checklist}>
          <li>
            <strong>Cadastros pelo link</strong>: quantas pessoas começaram pelo seu link.{' '}
            <strong>Viraram clientes</strong>: quantas pagaram. <strong>Veículos ativos</strong>: os que geram comissão
            hoje.
          </li>
          <li>
            <strong>Mês a mês</strong>: a comissão de cada mês, com a situação —{' '}
            {p.split ? (
              <>
                <em>A receber</em> (sai em cada pagamento) ou <em>Pago</em>.
              </>
            ) : (
              <>
                <em>Aguardando fechamento</em> (o mês ainda está correndo), <em>A pagar</em> (fechado, a Farbo vai pagar)
                e <em>Pago</em>.
              </>
            )}
          </li>
          <li>Os números atualizam a cada hora.</li>
          {report.protected ? (
            <li>
              <strong>Seus clientes</strong>: o nome de cada um, os veículos com a placa e o rastreador e o pagamento dos
              últimos meses (pago, em aberto ou vencido). Cliente com mensalidade vencida não gera comissão: vale um
              lembrete a ele.
            </li>
          ) : (
            <li>
              Para ver os nomes dos seus clientes e o pagamento de cada um, peça à Farbo para colocar senha na sua
              página.
            </li>
          )}
        </ul>
      </Step>

      <Step id="receber" n={n('receber')} title="Receber a comissão">
        {p.split ? (
          <>
            <p className={guide.text}>
              A sua comissão sai no próprio pagamento do cliente, pela Pagar.me, direto para a sua conta. Para isso, você
              se cadastra uma vez como recebedor:
            </p>
            <ol className={guide.numbered}>
              <li>
                Em <strong>Receba na hora, direto na sua conta</strong>, preencha os seus dados e a conta para receber.
                Eles vão direto para a Pagar.me: a Farbo não guarda nenhum deles.
              </li>
              <li>
                Faça a <strong>prova de vida</strong>: abra o link no celular (ou leia o QR Code), com o seu documento em
                mãos. O link vale 20 minutos; se vencer, gere outro.
              </li>
              <li>A Pagar.me analisa em até 2 dias úteis. A página mostra quando o cadastro ficar ativo.</li>
            </ol>
            <p className={guide.muted}>
              Ativo, a Pagar.me repassa todo dia. As comissões que estavam acumuladas vão junto nos próximos pagamentos.
            </p>
          </>
        ) : (
          <p className={guide.text}>
            No começo de cada mês, a Farbo fecha o mês anterior e paga a sua comissão por Pix. Confira em{' '}
            <strong>Mês a mês</strong>: o mês fechado aparece como <em>A pagar</em> e, depois do Pix, como <em>Pago</em>.
          </p>
        )}
      </Step>

      {p.kits && (
        <Step id="kits" n={n('kits')} title="Seus kits">
          <ol className={guide.numbered}>
            <li>
              Em <strong>Seus kits</strong>, use <em>Pedir kits</em>: a quantidade e, se quiser, uma observação. Para
              mudar a quantidade, cancele o pedido e peça de novo.
            </li>
            <li>A Farbo separa os rastreadores e despacha. O código de rastreio aparece na remessa.</li>
            <li>
              Chegou? Toque em <strong>Recebi os kits</strong>: os rastreadores (e os chips avulsos, se vierem) entram no
              seu estoque. Só os recebidos aparecem para instalar.
            </li>
            <li>
              Cada instalação baixa o rastreador do estoque sozinha. Para achar o da etiqueta, busque pelos últimos
              números do IMEI ou do chip.
            </li>
          </ol>
          <p className={guide.muted}>
            Normalmente o rastreador do kit já vem com o chip dentro. Quando o chip vier separado, ele aparece em{' '}
            <em>Chips avulsos no estoque</em> e é encaixado no rastreador na hora de ativar
            {p.installs ? (
              <>
                {' '}
                (veja <a href="#ativar">Ativar o rastreador</a>).
              </>
            ) : (
              '.'
            )}
          </p>
        </Step>
      )}

      {p.installs && (
        <>
          <Step id="agendar" n={n('agendar')} title="Agendar a instalação e a retirada">
            <ol className={guide.numbered}>
              <li>
                Em <strong>Instalações dos seus clientes</strong>, os clientes que pagaram aparecem em{' '}
                <em>Esperando a instalação</em>, com o telefone (toque para ligar) e o endereço. Quem escolheu instalar
                por conta própria não aparece.
              </li>
              <li>Combine o dia com o cliente e toque em <em>Agendar a instalação</em>: o dia, a hora e uma observação.</li>
              <li>O cliente recebe o aviso no app. A Farbo também vê o agendamento.</li>
              <li>
                Mudou o combinado? Use <em>Remarcar</em> (até você chegar no local) ou <em>Cancelar</em>.
              </li>
            </ol>
            <p className={guide.muted}>
              Quando um comodato acaba, o veículo aparece em <em>Esperando a retirada</em>: o agendamento é igual, e no
              dia você tira a foto do rastreador retirado e conclui.
            </p>
          </Step>

          <Step id="link-instalador" n={n('link-instalador')} title="No dia: o link do instalador">
            <p className={guide.text}>
              No agendamento, toque em <strong>Abrir o link do instalador</strong>. O seu link abre direto, sem senha, e
              funciona no celular sem login. Siga as etapas:
            </p>
            <ol className={guide.numbered}>
              <li>
                <strong>Chegada</strong>: no local, toque em <em>Cheguei no local</em>. A partir daí não dá mais para
                remarcar.
              </li>
              <li>
                <strong>Rastreador</strong>: escolha o rastreador do seu estoque pelos últimos números do IMEI da
                etiqueta. Se ele mostrar <em>Precisa ativar</em>, ative antes de instalar (veja{' '}
                <a href="#ativar">Ativar o rastreador</a>, a seguir). Com o rastreador instalado e ligado, toque em{' '}
                <em>Testar o sinal</em> e espere a confirmação de que ele está conectado e com GPS.
              </li>
              <li>
                <strong>Fotos</strong>: o rastreador instalado (com a etiqueta do IMEI aparecendo), o veículo inteiro (com
                a placa) e o painel ligado, sem luz de alerta acesa.
              </li>
              <li>
                <strong>Concluir</strong>: o rastreador fica ligado ao veículo do cliente na hora e sai do seu estoque.
              </li>
            </ol>
          </Step>

          <Step id="ativar" n={n('ativar')} title="Ativar o rastreador (antes de instalar)">
            <p className={guide.text}>
              O rastreador que nunca conectou precisa receber a configuração antes de ir para o veículo. Na lista do seu
              estoque, ele aparece como <em>Precisa ativar</em>. O que já mostra <em>Ativado</em> vai direto para a
              instalação e o teste do sinal.
            </p>
            <ol className={guide.numbered}>
              <li>
                No link do instalador, na etapa <strong>Rastreador</strong>, digite os últimos números do IMEI da etiqueta
                e toque no rastreador. Escolheu o errado? Toque em <em>Trocar</em>.
              </li>
              <li>
                Em <strong>Chip e ativação</strong>, escolha o chip:
                <ul className={guide.checklist}>
                  <li>
                    <strong>O chip já veio no rastreador</strong>: ele aparece primeiro, com “já está neste rastreador”.
                    Toque nele.
                  </li>
                  <li>
                    <strong>O chip veio separado</strong> (ou o rastreador veio sem chip): com o rastreador desligado,
                    encaixe o chip. Os chips avulsos do seu kit aparecem na lista: digite os últimos números do ICCID
                    impressos no chip e toque nele. Não está na lista? Digite <strong>todos os números do ICCID</strong>{' '}
                    (são 19 ou 20): o chip aparece sozinho, com a operadora, em <em>Chip escolhido</em>.
                  </li>
                </ul>
              </li>
              <li>
                Deixe o rastreador <strong>com energia</strong>, com o chip bem encaixado e num lugar com sinal de
                celular, e toque em <em>Ativar o rastreador</em>.
              </li>
              <li>
                Acompanhe na tela: <em>Mandando a configuração por SMS para o chip</em>, depois{' '}
                <em>Esperando o rastreador conectar</em> (pode levar alguns minutos) e, por fim,{' '}
                <em>Rastreador ativado e conectado</em>. Pode fechar a página: ao abrir o link de novo, a ativação
                aparece de onde parou.
              </li>
              <li>Ativado, instale o rastreador no veículo e siga para o teste do sinal.</li>
            </ol>
            <dl className={guide.faq}>
              <dt>“A configuração não chegou ao chip”</dt>
              <dd>Confira se o chip está bem encaixado e toque em <em>Ativar de novo</em>.</dd>
              <dt>“O rastreador não conectou em 20 minutos”</dt>
              <dd>
                Confira se o chip está bem encaixado, com sinal e crédito, e se o rastreador está ligado. Depois, ative
                de novo.
              </dd>
              <dt>“Este chip não está cadastrado na Farbo”</dt>
              <dd>
                Confira os números do ICCID no chip. Se estiverem certos, fale com a Farbo para cadastrar o chip antes de
                ativar.
              </dd>
              <dt>“ICCID inválido: o último dígito não confere”</dt>
              <dd>Algum número foi digitado errado: confira de novo, número por número.</dd>
              <dt>“Este chip já está no rastreador…”</dt>
              <dd>Esse chip já é de outro rastreador: use outro chip ou fale com a Farbo.</dd>
              <dt>“A ativação por SMS não está disponível agora”</dt>
              <dd>Fale com a Farbo antes de instalar.</dd>
            </dl>
            <div className={guide.warning} role="note">
              <strong>Cada link permite até 3 ativações</strong> (a tela mostra quantas restam). Uma tentativa em que o
              SMS nem saiu não conta. Acabaram as tentativas e o rastreador ainda não conectou? Fale com a Farbo antes
              de concluir a instalação.
            </div>
          </Step>
        </>
      )}

      {p.installer && (
        <Step id="instalar" n={n('instalar')} title="Ligar o rastreador no veículo">
          <ul className={guide.checklist}>
            <li>
              Instale num lugar <strong>escondido</strong>, com a face de cima do rastreador para cima e{' '}
              <strong>sem metal por cima</strong> (as antenas ficam dentro dele): atrás do painel ou embaixo do
              para-brisa. Prenda firme.
            </li>
            <li>Veículo desligado, chave fora da ignição. Emenda bem feita e isolada: emenda frouxa é a maior causa de falha.</li>
          </ul>
          <div className={guide.diagram}>
            <HarnessDiagram />
          </div>
          <table className={guide.wires}>
            <thead>
              <tr>
                <th>Fio</th>
                <th>Onde ligar</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <i className={guide.dot} style={{ background: '#dc2626' }} /> Vermelho
                </td>
                <td>
                  <strong>+12V permanente</strong> (tem energia mesmo com a chave desligada)
                </td>
              </tr>
              <tr>
                <td>
                  <i className={guide.dot} style={{ background: '#111827', boxShadow: '0 0 0 1px #64748b' }} /> Preto
                </td>
                <td>
                  <strong>Terra</strong>: negativo da bateria ou chassi sem tinta
                </td>
              </tr>
              <tr>
                <td>
                  <i className={guide.dot} style={{ background: '#ea580c' }} /> Laranja
                </td>
                <td>
                  <strong>Pós-chave</strong>: só tem energia com a ignição ligada
                </td>
              </tr>
              <tr>
                <td>
                  <i className={guide.dot} style={{ background: '#ca8a04' }} /> Amarelo
                </td>
                <td>
                  No <strong>amarelo do relê</strong> de bloqueio
                </td>
              </tr>
            </tbody>
          </table>
          <div className={guide.diagram}>
            <RelayDiagram />
          </div>
          <p className={guide.text}>
            No relê: o <strong>branco</strong> no +12V permanente, o <strong>amarelo</strong> no amarelo do rastreador e
            os dois <strong>verdes</strong> em série no fio da bomba de combustível (corte o fio e ligue um verde em cada
            ponta).
          </p>
          <div className={guide.warning} role="note">
            <strong>Atenção:</strong> o relê da caixa é de 12V — em caminhão ou ônibus (24V), fale com a Farbo antes. E o
            fio da bomba muda de veículo para veículo: na dúvida, não corte.
          </div>
          <p className={guide.text}>
            Para testar o bloqueio, com o veículo <strong>parado e em local seguro</strong>, peça ao cliente para tocar em{' '}
            <em>Desligar motor</em> no app e tente dar a partida: o motor não deve pegar. Depois, <em>Liberar motor</em>.
          </p>
          <Link to="/instalacao" className={guide.primary}>
            Ver o guia completo, com a animação
          </Link>
        </Step>
      )}

      {p.directPrices && (
        <Step id="valores" n={n('valores')} title="Seus valores de instalação">
          <p className={guide.text}>
            Você instala, desinstala e recebe direto do cliente. Em <strong>Seus valores de instalação</strong>, defina
            quanto cobra pela instalação e pela desinstalação: quem contrata pelo seu link vê esses valores no site e no
            app. Esse dinheiro não passa pela Farbo.
          </p>
        </Step>
      )}

      <Step id="app" n={n('app')} title="Ensinar o cliente a usar o app">
        <p className={guide.text}>Depois da instalação, mostre ao cliente o básico — ele confia mais quando vê funcionando:</p>
        <ul className={guide.checklist}>
          <li>
            <strong>Instalar o app</strong>: abrir o app pelo navegador do celular e escolher <em>Instalar app</em> ou{' '}
            <em>Adicionar à tela inicial</em> (no iPhone, em Compartilhar).
          </li>
          <li>
            <strong>Mapa</strong>: onde está cada veículo agora, se está ligado e quando mandou a última posição.
          </li>
          <li>
            <strong>Meus veículos</strong>: a posição atual, o trajeto de qualquer dia do histórico e os{' '}
            <strong>comandos</strong> — <em>Desligar motor</em> e <em>Liberar motor</em>. Por segurança, o bloqueio só é
            aceito com o veículo parado.
          </li>
          <li>
            <strong>Modo roubo</strong>: se o veículo for levado, ative no app. A posição passa a chegar mais vezes e sai
            um link para mandar à polícia.
          </li>
          <li>
            <strong>Alertas</strong>: avisos por e-mail de bateria desconectada, movimento com a ignição desligada,
            ignição ligada de madrugada e mais — o cliente escolhe o que receber.
          </li>
          <li>
            <strong>Cercas</strong>: marcar casa, trabalho ou o pátio da empresa e saber quando o veículo entra e sai.
          </li>
          <li>
            <strong>Relatórios</strong> e <strong>Manutenção</strong>: km rodados, viagens e excessos de velocidade; o
            que vence por km ou data e os documentos do veículo.
          </li>
          <li>
            <strong>Faturas</strong>: as mensalidades, para pagar em dia — é a mensalidade paga que gera a sua comissão.
          </li>
        </ul>
      </Step>

      <Step id="duvidas" n={n('duvidas')} title="Dúvidas comuns">
        <dl className={guide.faq}>
          <dt>O cliente se cadastrou, mas não aparece na minha página</dt>
          <dd>
            Os números atualizam a cada hora, e ele só vira cliente depois de pagar. Confira também se ele abriu o seu
            link (e não o de outra pessoa) antes de contratar.
          </dd>
          <dt>Quando a comissão de um cliente conta?</dt>
          <dd>Em cada mês em que a mensalidade do veículo dele é paga. Mês sem pagamento não gera comissão.</dd>
          {report.protected && (
            <>
              <dt>Esqueci a senha da minha página</dt>
              <dd>Peça à Farbo: ela troca a senha no seu cadastro. Depois de entrar, o aparelho fica liberado por 30 dias.</dd>
            </>
          )}
          {p.installs && (
            <>
              <dt>O rastreador não aparece para escolher no link do instalador</dt>
              <dd>
                Só aparecem os rastreadores do seu estoque que ainda não foram instalados.{' '}
                {p.kits ? 'Confira se você tocou em “Recebi os kits” quando a remessa chegou.' : 'Fale com a Farbo.'}
              </dd>
              <dt>O teste do sinal não passa</dt>
              <dd>
                Confira o vermelho (+12V permanente) e o preto (terra), ligue a ignição e deixe o veículo a céu aberto por
                alguns minutos: na garagem coberta, o GPS demora mais.
              </dd>
            </>
          )}
          <dt>Qual preço eu passo ao cliente?</dt>
          <dd>
            O que aparece no site pelo seu link: o flyer pronto já vem com ele. Na dúvida, mande o link, e o cliente vê o
            valor certo.
          </dd>
        </dl>
        {WHATSAPP_NUMBER ? (
          <a className={guide.whatsapp} href={whatsappUrl(HELP_MESSAGE)} target="_blank" rel="noopener noreferrer">
            Falar com a Farbo no WhatsApp
          </a>
        ) : null}
      </Step>

      {back}
    </>
  );
}

function Step({ id, n, title, children }: { id: string; n: number; title: string; children: ReactNode }) {
  return (
    <section id={id} className={guide.card} aria-labelledby={`${id}-titulo`}>
      <h2 id={`${id}-titulo`} className={guide.heading}>
        <span className={guide.step}>{n}</span> {title}
      </h2>
      {children}
    </section>
  );
}
