import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import { HarnessDiagram, RelayDiagram } from '@/components/install/J16Wiring';
import { ClientAreaLink } from '@/components/landing/ClientAreaLink';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { CONTACT_EMAIL, WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';

import base from './PreLaunch.module.css';
import styles from './InstallGuide.module.css';

const HELP_MESSAGE = 'Olá! Recebi o rastreador J16 e preciso de ajuda com a instalação.';

const STEPS = [
  { id: 'local', label: 'Onde instalar' },
  { id: 'chicote', label: 'Ligar o chicote' },
  { id: 'rele', label: 'Relê de bloqueio' },
  { id: 'teste', label: 'Testar' },
];

/**
 * Os próximos passos de quem recebe o rastreador J16 em casa
 * (/instalacao, o QR Code do folheto da embalagem): o que vem na caixa, quem
 * instala (um prestador parceiro, de preferência), o passo a passo da
 * ligação — que serve ao instalador e a quem tem experiência —, o teste pelo
 * app e o que fazer depois. O rastreador já chega configurado, com o chip
 * ativado: é só instalar.
 *
 * A ligação vem do manual do fabricante (família GT06/Concox): o relê de 12V
 * com o branco no +12V, o amarelo no rastreador e os verdes em série no fio da
 * bomba de combustível.
 */
export function InstallGuidePage() {
  const [installers, setInstallers] = useState(false);

  useEffect(() => {
    document.title = 'Instalação do rastreador · Farbo Rastreadores';
  }, []);

  return (
    <main className={`${base.page} ${styles.page}`} data-theme="dark">
      <header className={base.header}>
        <Link to="/" aria-label="Farbo Rastreadores">
          <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        </Link>
        <span className={base.badge}>Instalação</span>
      </header>

      <section className={styles.hero}>
        <img
          src="/assets/rastreador-j16-farbo.webp"
          alt="Rastreador J16 da Farbo"
          className={styles.heroImage}
          width={160}
          height={160}
        />
        <div>
          <h1 className={styles.title}>Seu rastreador chegou!</h1>
          <p className={styles.lead}>
            O J16 já vem configurado, com o chip de dados ativado: é só instalar no veículo. Assim que ele ligar, o
            veículo aparece no app.
          </p>
        </div>
      </section>

      <section className={styles.card} aria-labelledby="caixa">
        <h2 id="caixa" className={styles.heading}>
          O que vem na caixa
        </h2>
        <ul className={styles.box}>
          <li>
            <strong>Rastreador J16</strong>
            <span>com o chip de dados já instalado e ativado</span>
          </li>
          <li>
            <strong>Chicote de instalação</strong>
            <span>4 fios: vermelho, preto, laranja e amarelo</span>
          </li>
          <li>
            <strong>Mini relê de bloqueio (12V)</strong>
            <span>4 fios: branco, amarelo e dois verdes</span>
          </li>
        </ul>
        <p className={styles.muted}>Faltou alguma coisa? Fale com a gente antes de instalar.</p>
      </section>

      <section className={`${styles.card} ${styles.highlight}`} aria-labelledby="quem">
        <h2 id="quem" className={styles.heading}>
          Quem instala
        </h2>
        <p className={styles.text}>
          <strong>Recomendamos um instalador parceiro:</strong> ele faz a instalação escondida e liga o bloqueio com
          segurança. Você combina horário e valor pelo WhatsApp e paga direto a ele.
        </p>
        <button type="button" className={styles.primary} onClick={() => setInstallers(true)}>
          Ver os instaladores parceiros
        </button>
        <p className={styles.text}>
          Tem experiência com elétrica automotiva? O passo a passo abaixo é o mesmo que o instalador segue. Na dúvida,
          chame um profissional: uma ligação errada pode danificar o veículo.
        </p>
      </section>

      <section className={styles.card} aria-labelledby="antes">
        <h2 id="antes" className={styles.heading}>
          Antes de começar
        </h2>
        <ul className={styles.checklist}>
          <li>Veículo desligado, chave fora da ignição.</li>
          <li>Alicate, fita isolante (ou terminais), abraçadeiras e, de preferência, um multímetro para achar os fios.</li>
          <li>
            O rastreador funciona em 12V e 24V, mas o <strong>relê que vem na caixa é de 12V</strong>: em caminhão ou
            ônibus (24V), não use esse relê — fale com a gente.
          </li>
        </ul>
        <nav className={styles.index} aria-label="Etapas">
          {STEPS.map((step, i) => (
            <a key={step.id} href={`#${step.id}`}>
              <span>{i + 1}</span>
              {step.label}
            </a>
          ))}
        </nav>
      </section>

      <section id="local" className={styles.card} aria-labelledby="local-titulo">
        <h2 id="local-titulo" className={styles.heading}>
          <span className={styles.step}>1</span> Onde instalar
        </h2>
        <ul className={styles.checklist}>
          <li>
            Num lugar <strong>escondido</strong>: quanto menos gente souber onde está, mais difícil de um ladrão achar.
          </li>
          <li>
            Com a <strong>face de cima do rastreador virada para cima e sem metal por cima</strong>: as antenas de GPS e
            de celular ficam dentro dele.
          </li>
          <li>
            Bons lugares: atrás do painel (na parte de plástico), embaixo do para-brisa dianteiro ou do traseiro.
          </li>
          <li>
            Longe de aparelhos de corrente alta (alarme, rádio amplificado, radar). Película metalizada no para-brisa
            atrapalha o sinal: nesse caso, escolha outro lugar.
          </li>
          <li>Prenda firme, com abraçadeiras ou fita dupla face larga, para não balançar.</li>
        </ul>
      </section>

      <section id="chicote" className={styles.card} aria-labelledby="chicote-h">
        <h2 id="chicote-h" className={styles.heading}>
          <span className={styles.step}>2</span> Ligar o chicote
        </h2>
        <div className={styles.diagram}>
          <HarnessDiagram />
        </div>
        <table className={styles.wires}>
          <thead>
            <tr>
              <th>Fio</th>
              <th>Onde ligar</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#dc2626' }} /> Vermelho
              </td>
              <td>
                <strong>+12V permanente</strong>: positivo da bateria ou um fio que tenha energia mesmo com a chave
                desligada.
              </td>
            </tr>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#111827', boxShadow: '0 0 0 1px #64748b' }} /> Preto
              </td>
              <td>
                <strong>Terra</strong>: negativo da bateria ou um ponto do chassi sem tinta.
              </td>
            </tr>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#ea580c' }} /> Laranja
              </td>
              <td>
                <strong>Pós-chave</strong>: um fio que só tem energia com a ignição ligada. É por ele que o app sabe
                quando o veículo está ligado.
              </td>
            </tr>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#ca8a04' }} /> Amarelo
              </td>
              <td>
                <strong>Relê de bloqueio</strong>: vai no fio amarelo do relê (próximo passo). Se não for instalar o
                bloqueio agora, isole bem a ponta.
              </td>
            </tr>
          </tbody>
        </table>
        <p className={styles.muted}>Isole cada emenda com solda e fita, ou com terminais: emenda frouxa é a maior causa de falha.</p>
      </section>

      <section id="rele" className={styles.card} aria-labelledby="rele-h">
        <h2 id="rele-h" className={styles.heading}>
          <span className={styles.step}>3</span> Instalar o relê de bloqueio
        </h2>
        <p className={styles.text}>
          O relê é o que permite <strong>desligar o motor pelo app</strong> em caso de roubo. Ele fica fechado o tempo
          todo: o veículo funciona normalmente e só deixa de funcionar quando você manda bloquear.
        </p>
        <div className={styles.diagram}>
          <RelayDiagram />
        </div>
        <table className={styles.wires}>
          <thead>
            <tr>
              <th>Fio do relê</th>
              <th>Onde ligar</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#f8fafc', boxShadow: '0 0 0 1px #64748b' }} /> Branco
                (fino)
              </td>
              <td>
                <strong>+12V permanente</strong>, o mesmo do fio vermelho do rastreador.
              </td>
            </tr>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#ca8a04' }} /> Amarelo
              </td>
              <td>
                No <strong>fio amarelo do chicote do rastreador</strong>.
              </td>
            </tr>
            <tr>
              <td>
                <i className={styles.dot} style={{ background: '#16a34a' }} /> Os dois verdes (grossos)
              </td>
              <td>
                Corte o <strong>fio de alimentação da bomba de combustível</strong> e ligue um verde em cada ponta (tanto
                faz qual): o relê fica no meio do fio.
              </td>
            </tr>
          </tbody>
        </table>
        <div className={styles.warning} role="note">
          <strong>Atenção:</strong> o fio da bomba de combustível muda de veículo para veículo. Se não tiver certeza de
          qual é, chame um instalador — cortar o fio errado pode deixar o veículo sem funcionar ou desligar algo
          importante.
        </div>
      </section>

      <section id="teste" className={styles.card} aria-labelledby="teste-h">
        <h2 id="teste-h" className={styles.heading}>
          <span className={styles.step}>4</span> Testar
        </h2>
        <ol className={styles.numbered}>
          <li>
            Ligue a ignição e deixe o veículo <strong>a céu aberto</strong> por alguns minutos (na garagem coberta, o GPS
            demora mais).
          </li>
          <li>Abra o app: o veículo deve aparecer online, com a posição no mapa.</li>
          <li>Dê uma volta curta e confira se o trajeto aparece no app.</li>
          <li>
            Teste o bloqueio <strong>com o veículo parado, em local seguro</strong>: em Comandos, toque em{' '}
            <em>Desligar motor</em> e tente dar a partida — o motor não deve pegar. Depois toque em{' '}
            <em>Liberar motor</em> e confira que ele volta a funcionar.
          </li>
        </ol>
        <p className={styles.muted}>
          Por segurança, o bloqueio pelo app só é aceito com o veículo parado: ele nunca desliga o motor em movimento.
        </p>
        <ClientAreaLink className={styles.primary}>Abrir o app</ClientAreaLink>
      </section>

      <section className={styles.card} aria-labelledby="depois">
        <h2 id="depois" className={styles.heading}>
          Pronto! E agora?
        </h2>
        <ul className={styles.checklist}>
          <li>
            <strong>Alertas</strong>: avise no celular quando o veículo ligar, passar do limite de velocidade ou sair de
            uma cerca.
          </li>
          <li>
            <strong>Cercas</strong>: marque casa, trabalho ou o pátio da empresa e saiba quando o veículo entra e sai.
          </li>
          <li>
            <strong>Modo roubo</strong>: se o veículo for levado, ative no app — a posição passa a chegar mais vezes e
            você recebe um link para mandar à polícia.
          </li>
          <li>
            <strong>Instale o app no celular</strong>: abra o app pelo navegador do celular e escolha{' '}
            <em>Instalar app</em> ou <em>Adicionar à tela inicial</em> (no iPhone, em Compartilhar).
          </li>
        </ul>
      </section>

      <section className={styles.card} aria-labelledby="problemas">
        <h2 id="problemas" className={styles.heading}>
          Se algo não funcionar
        </h2>
        <dl className={styles.faq}>
          <dt>O veículo não aparece online</dt>
          <dd>
            Confira o vermelho (+12V permanente) e o preto (terra), ligue a ignição e deixe o veículo a céu aberto por
            uns 10 minutos.
          </dd>
          <dt>Aparece online, mas a posição não atualiza</dt>
          <dd>O rastreador está sem sinal de GPS: mude o local, com a face de cima livre e sem metal por cima.</dd>
          <dt>O app não mostra quando o veículo liga</dt>
          <dd>Confira o laranja: ele precisa estar num fio que só tem energia com a ignição ligada.</dd>
          <dt>O veículo não pega depois da instalação</dt>
          <dd>
            Confira os verdes do relê: cada ponta do fio cortado vai num verde, bem emendada. Confira também o branco no
            +12V permanente.
          </dd>
          <dt>O bloqueio não funciona</dt>
          <dd>Confira o amarelo do relê no amarelo do rastreador e o branco no +12V permanente.</dd>
        </dl>
        {WHATSAPP_NUMBER ? (
          <a className={styles.whatsapp} href={whatsappUrl(HELP_MESSAGE)} target="_blank" rel="noopener noreferrer">
            Falar com a Farbo no WhatsApp
          </a>
        ) : null}
        <p className={styles.muted}>
          Ou escreva para <a href={`mailto:${CONTACT_EMAIL}`}>{CONTACT_EMAIL}</a>.
        </p>
      </section>

      <footer className={styles.footer}>
        <Link to="/termos-de-uso">Termos de uso</Link>
        <Link to="/politica-de-privacidade">Política de privacidade</Link>
      </footer>

      <InstallersModal isOpen={installers} onClose={() => setInstallers(false)} />
    </main>
  );
}
