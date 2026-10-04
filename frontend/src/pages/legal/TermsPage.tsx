import { Link } from 'react-router-dom';

import { COMPANY, LEGAL_FACTS, PRIVACY_PATH } from '@/config/legal';
import { INSANOS_MONTHLY, LAUNCH_OFFER, PRE_LAUNCH } from '@/config/landing';

import styles from './Legal.module.css';
import { CompanyIdentity, ContactChannels, LegalLayout } from './LegalLayout';
import type { LegalSection } from './LegalLayout';

const SECTIONS: LegalSection[] = [
  {
    id: 'aceitacao',
    title: 'Quem somos e a aceitação destes termos',
    body: (
      <>
        <p>
          Estes Termos de Uso regem o uso do site, do painel, do app e do serviço de rastreamento veicular de{' '}
          <CompanyIdentity />. Ao criar a conta, contratar ou usar o serviço, você declara que leu e concorda
          com estes termos e com a <Link to={PRIVACY_PATH}>Política de Privacidade</Link>.
        </p>
        <p>
          Dúvidas? Fale com a gente <ContactChannels />.
        </p>
      </>
    ),
  },
  {
    id: 'definicoes',
    title: 'Definições',
    body: (
      <ul>
        <li>
          <strong>Cliente</strong>: quem contrata o serviço e é o titular da conta.
        </li>
        <li>
          <strong>Rastreador</strong>: o equipamento instalado no veículo, homologado pela Anatel, com chip de
          dados móveis.
        </li>
        <li>
          <strong>Assinatura</strong>: o plano mensal que dá acesso ao rastreamento de um veículo.
        </li>
        <li>
          <strong>Plataforma</strong>: o painel na web e o app no celular.
        </li>
        <li>
          <strong>Pessoa autorizada</strong>: quem o Cliente autoriza, pela plataforma, a acompanhar um veículo
          dele.
        </li>
      </ul>
    ),
  },
  {
    id: 'servico',
    title: 'O serviço',
    body: (
      <>
        <p>
          Com o rastreador instalado, o Cliente acompanha o veículo em tempo real na plataforma. O serviço
          também inclui o histórico de trajetos, os alertas no celular e por e-mail, as cercas virtuais, o
          bloqueio remoto do motor (com o relé instalado) e a autorização de outras pessoas.
        </p>
        <p>O funcionamento depende de fatores que fogem ao nosso controle, entre eles:</p>
        <ul>
          <li>a cobertura das redes de telefonia móvel e o sinal de GPS no local;</li>
          <li>a alimentação elétrica do veículo e a instalação correta do rastreador;</li>
          <li>serviços de terceiros, como operadoras, mapas e notificações do celular.</li>
        </ul>
        <p>
          Em garagens subterrâneas, túneis ou áreas sem cobertura, a posição pode atrasar ou ficar imprecisa.
        </p>
        <div className={styles.callout}>
          <p>
            O rastreamento ajuda a localizar e proteger o veículo, mas <strong>não é seguro</strong> e não
            garante a recuperação em caso de roubo ou furto.
          </p>
        </div>
      </>
    ),
  },
  {
    id: 'conta',
    title: 'Cadastro e conta',
    body: (
      <ul>
        <li>A conta é pessoal e só pode ser criada por maiores de 18 anos.</li>
        <li>O Cliente deve informar dados verdadeiros e mantê-los atualizados, inclusive o endereço de entrega.</li>
        <li>
          A senha e o acesso por biometria são pessoais. O Cliente é responsável pelo que for feito na conta e
          deve nos avisar logo se suspeitar de uso indevido.
        </li>
      </ul>
    ),
  },
  {
    id: 'contratacao',
    title: 'Contratação e preços',
    body: (
      <>
        <p>
          A contratação é feita pela plataforma, em <strong>Novo veículo</strong>: o Cliente informa o veículo
          e, no mesmo pedido, compra o rastreador (pagamento único) e inicia a assinatura. Cada assinatura cobre
          um veículo. Os preços são os da tabela vigente, mostrados no site e no próprio pedido antes da
          confirmação.
        </p>
        {PRE_LAUNCH && (
          <>
            <h3>Promoção de pré-lançamento</h3>
            <p>
              Quem se inscreve na lista de pré-lançamento contrata o primeiro rastreador por{' '}
              {LAUNCH_OFFER.equipment} e paga {LAUNCH_OFFER.monthly} de mensalidade nos {LAUNCH_OFFER.months}{' '}
              primeiros meses. Para os integrantes do Insanos MC, a mensalidade da promoção é{' '}
              {LAUNCH_OFFER.insanosMonthly}.
            </p>
            <p>
              Depois desse período, vale o preço do plano ({LAUNCH_OFFER.monthlyRegular}, ou{' '}
              {INSANOS_MONTHLY.label} para o Insanos MC).
            </p>
            <p>Regras da promoção:</p>
            <ul>
              <li>vale para um veículo por Cliente;</li>
              <li>é limitada aos {LAUNCH_OFFER.slots} primeiros Clientes da lista a contratar;</li>
              <li>exige que a conta use o mesmo e-mail da inscrição.</li>
            </ul>
          </>
        )}
        <p>
          Mudanças de preço da mensalidade são avisadas por e-mail com pelo menos 30 dias de antecedência e não
          alteram as condições promocionais já contratadas durante o seu prazo.
        </p>
      </>
    ),
  },
  {
    id: 'pagamento',
    title: 'Pagamento e atraso',
    body: (
      <ul>
        <li>As faturas ficam disponíveis na plataforma antes do vencimento e podem ser pagas por Pix.</li>
        <li>
          Com fatura vencida há mais de {LEGAL_FACTS.suspendAfterDays} dias, o acesso do Cliente à plataforma
          fica suspenso até o pagamento. O rastreador continua registrando as posições, que voltam a aparecer
          quando o acesso é liberado.
        </li>
        <li>Em atraso prolongado, a assinatura pode ser encerrada, com aviso prévio por e-mail.</li>
      </ul>
    ),
  },
  {
    id: 'entrega-instalacao',
    title: 'Entrega e instalação',
    body: (
      <>
        <p>
          O rastreador é enviado ao endereço de entrega com código de rastreio da transportadora, ou entregue
          em mãos quando combinado. O prazo de entrega é informado no pedido.
        </p>
        <p>
          A instalação é feita por prestadores parceiros independentes, que a plataforma indica. O Cliente
          combina horário e valor e paga direto ao prestador. Recomendamos sempre a instalação por um
          profissional; o bloqueio do motor exige o relé instalado.
        </p>
      </>
    ),
  },
  {
    id: 'arrependimento',
    title: 'Direito de arrependimento',
    body: (
      <p>
        Como a compra é feita pela internet, o Cliente pode desistir em até 7 dias a contar do recebimento do
        rastreador, como garante o artigo 49 do Código de Defesa do Consumidor. Basta nos avisar pelos canais
        de contato. Devolvemos os valores pagos e combinamos a devolução do equipamento.
      </p>
    ),
  },
  {
    id: 'garantia',
    title: 'Garantia do rastreador',
    body: (
      <p>
        O rastreador tem a garantia legal de 90 dias contra defeitos, a contar da entrega, nos termos do Código
        de Defesa do Consumidor. A garantia não cobre danos causados por mau uso, acidente, contato com água,
        violação do equipamento ou intervenção de pessoas não autorizadas. Para acionar, fale com a gente pelos
        canais de contato.
      </p>
    ),
  },
  {
    id: 'bloqueio',
    title: 'Bloqueio remoto do motor',
    body: (
      <>
        <div className={styles.callout}>
          <p>
            Em caso de roubo ou furto, <strong>acione a polícia primeiro (190)</strong>. Nunca vá atrás do
            veículo nem coloque a sua vida ou a de outras pessoas em risco.
          </p>
        </div>
        <ul>
          <li>O bloqueio só pode ser usado em veículo do Cliente ou que ele esteja autorizado a controlar.</li>
          <li>
            Por segurança, o sistema só envia o bloqueio com o veículo parado ou em velocidade muito baixa e com
            posição recente. Fora dessa condição, o pedido é recusado.
          </li>
          <li>Bloquear e desbloquear exigem a confirmação de que é o Cliente (biometria ou senha).</li>
          <li>O desbloqueio é feito pelo Cliente ou pela nossa central.</li>
          <li>
            A execução depende do sinal do rastreador e do relé instalado. A plataforma mostra quando o
            rastreador confirma o comando.
          </li>
        </ul>
      </>
    ),
  },
  {
    id: 'terceiros',
    title: 'Pessoas autorizadas',
    body: (
      <>
        <p>
          O Cliente pode autorizar até {LEGAL_FACTS.sharesPerVehicle} pessoas por veículo, cada uma com a
          própria conta.
        </p>
        <ul>
          <li>Elas veem a posição ao vivo.</li>
          <li>
            Se o Cliente permitir, também podem bloquear o motor numa emergência, com a mesma regra de segurança.
            Elas nunca desbloqueiam.
          </li>
          <li>O Cliente recebe um aviso quando dá um acesso e quando alguém bloqueia.</li>
          <li>O Cliente pode retirar qualquer acesso a qualquer momento.</li>
          <li>O Cliente responde por quem autoriza.</li>
        </ul>
      </>
    ),
  },
  {
    id: 'uso-proibido',
    title: 'Usos proibidos',
    body: (
      <>
        <p>É proibido:</p>
        <ul>
          <li>
            usar o serviço para vigiar ou perseguir pessoas sem o conhecimento delas (o que pode configurar
            crime, como o de perseguição, previsto na Lei nº 14.132/2021);
          </li>
          <li>instalar o rastreador em veículo de outra pessoa sem a autorização dela;</li>
          <li>tentar acessar contas, veículos ou dados que não são seus, ou burlar as travas de segurança;</li>
          <li>copiar, revender ou explorar a plataforma, ou fazer engenharia reversa dela;</li>
          <li>usar o serviço para qualquer fim ilegal.</li>
        </ul>
        <p>
          O Cliente deve avisar as pessoas que usam o veículo com frequência de que ele é rastreado. O uso
          proibido leva à suspensão ou ao encerramento da conta, sem prejuízo das medidas legais.
        </p>
      </>
    ),
  },
  {
    id: 'disponibilidade',
    title: 'Disponibilidade da plataforma',
    body: (
      <p>
        Trabalhamos para manter a plataforma disponível o tempo todo, mas ela pode passar por manutenções e
        interrupções eventuais. Quando possível, avisamos as manutenções programadas com antecedência.
      </p>
    ),
  },
  {
    id: 'responsabilidade',
    title: 'Responsabilidades',
    body: (
      <>
        <p>
          Respondemos pelo serviço nos termos do Código de Defesa do Consumidor. Na medida permitida pela lei,
          não respondemos por danos decorrentes:
        </p>
        <ul>
          <li>do uso do serviço em desacordo com estes termos;</li>
          <li>de informações erradas fornecidas pelo Cliente;</li>
          <li>de falhas de redes e serviços de terceiros fora do nosso controle;</li>
          <li>de caso fortuito ou força maior.</li>
        </ul>
      </>
    ),
  },
  {
    id: 'cancelamento',
    title: 'Cancelamento',
    body: (
      <>
        <p>
          O Cliente pode cancelar a assinatura a qualquer momento, sem multa, pelos canais de contato.
        </p>
        <ul>
          <li>Faturas já vencidas continuam devidas.</li>
          <li>O rastreador comprado é do Cliente.</li>
          <li>
            Depois do cancelamento, os dados seguem o que diz a{' '}
            <Link to={PRIVACY_PATH}>Política de Privacidade</Link>.
          </li>
        </ul>
        <p>
          Podemos encerrar a conta em caso de uso proibido ou de atraso prolongado no pagamento, com aviso
          prévio sempre que possível.
        </p>
      </>
    ),
  },
  {
    id: 'propriedade',
    title: 'Marca e propriedade intelectual',
    body: (
      <p>
        A marca {COMPANY.name}, os logotipos, o site, o painel e o app são de nossa titularidade ou licenciados
        para nós. O uso do serviço não transfere nenhum desses direitos ao Cliente.
      </p>
    ),
  },
  {
    id: 'comunicacoes',
    title: 'Comunicações',
    body: (
      <p>
        Falamos com o Cliente pelo e-mail cadastrado, pelas notificações da plataforma e pelo WhatsApp.
        Convites, avisos de segurança, faturas e mudanças nestes termos são enviados por e-mail.
      </p>
    ),
  },
  {
    id: 'alteracoes',
    title: 'Mudanças nestes termos',
    body: (
      <p>
        Podemos atualizar estes termos para refletir mudanças no serviço ou na lei. A versão em vigor fica
        nesta página, com a data no topo. Mudanças relevantes são avisadas por e-mail com antecedência.
        Continuar usando o serviço depois disso significa concordar com a nova versão; se o Cliente não
        concordar, pode cancelar sem multa.
      </p>
    ),
  },
  {
    id: 'foro',
    title: 'Lei aplicável e foro',
    body: (
      <p>
        Estes termos seguem a legislação brasileira. Fica eleito o foro do domicílio do Cliente para resolver
        qualquer questão relacionada a eles, como prevê o Código de Defesa do Consumidor.
      </p>
    ),
  },
];

export function TermsPage() {
  return (
    <LegalLayout
      title="Termos de Uso"
      intro={
        <p className={styles.callout}>
          Estes são os combinados entre a {COMPANY.name} e quem usa o nosso rastreamento: o que oferecemos, o que
          esperamos de você e como resolvemos qualquer questão.
        </p>
      }
      sections={SECTIONS}
    />
  );
}
