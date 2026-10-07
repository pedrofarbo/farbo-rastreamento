import { Link } from 'react-router-dom';

import { COMPANY, LEGAL_FACTS, TERMS_PATH } from '@/config/legal';

import styles from './Legal.module.css';
import { CompanyIdentity, ContactChannels, LegalLayout } from './LegalLayout';
import type { LegalSection } from './LegalLayout';

const history = LEGAL_FACTS.historyOptions.join(', ').replace(/, (\d+)$/, ' ou $1');

const SECTIONS: LegalSection[] = [
  {
    id: 'quem-somos',
    title: 'Quem somos e como falar com a gente',
    body: (
      <>
        <p>
          Esta política explica como <CompanyIdentity /> trata os dados pessoais de quem visita o site, faz
          o pré-cadastro e usa o rastreamento, o painel e o app. Somos a controladora desses dados, nos termos
          da Lei Geral de Proteção de Dados (LGPD, Lei nº 13.709/2018).
        </p>
        <p>
          Para qualquer assunto de privacidade, inclusive falar com o encarregado pelo tratamento de dados,
          fale com a gente <ContactChannels />.
        </p>
      </>
    ),
  },
  {
    id: 'resumo',
    title: 'O essencial',
    body: (
      <ul>
        <li>Não vendemos nem alugamos os seus dados.</li>
        <li>O site não usa cookies de rastreamento nem ferramentas de publicidade.</li>
        <li>A localização do veículo é usada para prestar o serviço que você contratou.</li>
        <li>
          Você escolhe por quanto tempo o histórico de posições do veículo fica guardado ({history} dias);
          depois disso, ele é apagado.
        </li>
        <li>Ninguém além de você acompanha o seu veículo, a não ser quem você autorizar.</li>
        <li>Você pode pedir acesso, correção ou exclusão dos seus dados a qualquer momento.</li>
      </ul>
    ),
  },
  {
    id: 'dados',
    title: 'Quais dados tratamos',
    body: (
      <>
        <h3>Quando você visita o site</h3>
        <p>
          Contamos as visitas sem cookies e sem identificar ninguém. Registramos a página, as partes dela a
          que você chega, os botões em que clica, de onde veio (só o endereço do site de origem e a campanha
          do link, se houver) e o tipo de aparelho, navegador e sistema. Para contar cada visitante uma vez
          por dia, o servidor gera um código a partir do endereço IP e do navegador, misturados a uma chave
          que muda todo dia. O IP não é guardado, e a chave do dia anterior é apagada: o código não permite
          saber quem você é nem acompanhar você de um dia para o outro. Esses registros ficam guardados por{' '}
          {LEGAL_FACTS.analyticsDays} dias.
        </p>
        <p>
          As fontes de letra do site são carregadas do Google Fonts, que recebe o endereço IP do seu
          navegador para entregar os arquivos.
        </p>

        <h3>No pré-cadastro e na lista de pré-lançamento</h3>
        <p>
          Nome, e-mail, WhatsApp, cidade, plano e tipo e quantidade de veículos, a mensagem (se você escrever
          uma) e a data em que você aceitou ser contatado. Na lista de pré-lançamento: nome (opcional),
          e-mail, WhatsApp e a data do aceite.
        </p>

        <h3>Na sua conta</h3>
        <p>
          Nome, e-mail, telefone, CPF ou CNPJ e o endereço de entrega. A senha é guardada cifrada: nem a
          nossa equipe consegue lê-la.
        </p>

        <h3>No veículo e no rastreamento</h3>
        <ul>
          <li>Dados do veículo: nome, placa, marca, modelo, ano e cor.</li>
          <li>
            O que o rastreador envia: posição (latitude e longitude), velocidade, direção, altitude, ignição,
            situação do motor (bloqueado ou liberado), bateria e sinal, além dos eventos e alertas (por
            exemplo, ignição ligada, saída de uma cerca, bateria desconectada).
          </li>
          <li>Os comandos enviados ao rastreador (como bloqueio e desbloqueio), com quem pediu e quando.</li>
          <li>As cercas e as preferências de alerta que você cadastrar.</li>
        </ul>

        <h3>No pagamento</h3>
        <p>
          As faturas, os pagamentos e a situação da assinatura. Não recebemos nem guardamos dados de cartão.
        </p>

        <h3>No atendimento</h3>
        <p>
          As mensagens trocadas por e-mail e pelo WhatsApp, e o histórico do atendimento. Quando o
          atendimento automático estiver ativo no WhatsApp, as mensagens são respondidas por um assistente
          de inteligência artificial; você pode pedir para falar com uma pessoa da equipe a qualquer momento.
        </p>

        <h3>Na segurança da conta</h3>
        <ul>
          <li>Registros de acesso e de ações no painel e no app: data, hora, endereço IP e a ação feita.</li>
          <li>
            Biometria (Face ID, digital): a sua biometria nunca sai do seu aparelho. Guardamos só uma chave
            pública, que serve para confirmar que foi você, sem revelar nada sobre a sua digital ou o seu
            rosto.
          </li>
          <li>A inscrição do aparelho para receber notificações, se você ativar.</li>
        </ul>

        <h3>Quando você dá acesso a outra pessoa</h3>
        <p>
          Se você autorizar alguém a acompanhar o seu veículo, guardamos o nome e o e-mail dessa pessoa, o
          veículo e o que ela pode fazer. Ela vê a posição ao vivo e, se você permitir, pode bloquear o
          motor numa emergência; o histórico e os eventos continuam só com você.
        </p>
      </>
    ),
  },
  {
    id: 'finalidades',
    title: 'Para que usamos os dados e com que base legal',
    body: (
      <ul>
        <li>
          <strong>Prestar o serviço contratado</strong>: mostrar o veículo no mapa, guardar o histórico,
          mandar alertas, enviar ao rastreador os comandos que você pedir, entregar o rastreador e dar suporte
          (execução de contrato).
        </li>
        <li>
          <strong>Cobrar e cumprir obrigações fiscais e legais</strong>: faturas, comprovantes, as notas
          fiscais (a NF-e do rastreador e a NFS-e das mensalidades, emitidas com o CPF ou CNPJ), o registro do
          aceite do contrato (data, hora, IP e navegador) e os registros exigidos por lei (execução de contrato
          e cumprimento de obrigação legal).
        </li>
        <li>
          <strong>Proteger a sua conta e o serviço</strong>: prevenir fraudes e acessos indevidos e guardar os
          registros de acesso exigidos pelo Marco Civil da Internet (legítimo interesse e obrigação legal).
        </li>
        <li>
          <strong>Responder ao pré-cadastro e avisar do lançamento</strong> (consentimento, que você pode
          retirar quando quiser).
        </li>
        <li>
          <strong>Entender como o site é usado</strong>, com as contagens anônimas descritas acima, para
          melhorá-lo (legítimo interesse).
        </li>
      </ul>
    ),
  },
  {
    id: 'compartilhamento',
    title: 'Com quem compartilhamos',
    body: (
      <>
        <p>
          Só com quem precisa dos dados para o serviço funcionar, e apenas o necessário. Esses fornecedores
          tratam os dados em nosso nome e seguindo as nossas instruções:
        </p>
        <ul>
          <li>
            <strong>Hospedagem</strong>: os servidores onde o sistema e o banco de dados ficam.
          </li>
          <li>
            <strong>AbacatePay</strong>, para gerar e confirmar os pagamentos por Pix (nome, e-mail, CPF ou
            CNPJ e celular).
          </li>
          <li>
            <strong>Melhor Envios e a transportadora</strong>, para emitir a etiqueta e entregar o rastreador
            (nome, endereço, telefone, e-mail e CPF ou CNPJ).
          </li>
          <li>
            <strong>Provedor de e-mail</strong>, para mandar convites, alertas e avisos.
          </li>
          <li>
            <strong>Emissão de notas fiscais</strong>: o sistema emissor e os órgãos fiscais (prefeitura e
            Secretaria da Fazenda), para emitir a NF-e e a NFS-e (nome, CPF ou CNPJ, endereço e valores).
          </li>
          <li>
            <strong>Órgãos de proteção ao crédito</strong> (como SPC e Serasa), só em caso de atraso de mais de
            1 mês, com aviso prévio, conforme o contrato.
          </li>
          <li>
            <strong>WhatsApp (Meta)</strong>, no atendimento por WhatsApp, e o provedor de inteligência
            artificial (Anthropic), quando o atendimento automático estiver ativo.
          </li>
          <li>
            <strong>OpenStreetMap</strong>: os mapas do painel e do app são carregados dele pelo seu navegador,
            e o nosso servidor consulta o serviço de endereços dele com as coordenadas da posição (sem nome nem
            outros dados seus) para mostrar o endereço.
          </li>
          <li>
            <strong>Serviços de notificação</strong> do navegador ou do sistema do celular, para entregar os
            alertas no aparelho, se você ativar.
          </li>
        </ul>
        <p>
          Os prestadores de instalação recomendados são independentes: o contato com eles acontece direto
          entre você e o prestador, e não passamos os seus dados a eles.
        </p>
        <p>
          Também podemos fornecer dados a autoridades quando a lei exigir ou houver ordem judicial. Em caso
          de roubo ou furto, a localização do veículo só é passada às autoridades a seu pedido ou por
          determinação legal.
        </p>
      </>
    ),
  },
  {
    id: 'internacional',
    title: 'Transferência para fora do Brasil',
    body: (
      <p>
        Alguns fornecedores (como o WhatsApp, o provedor de inteligência artificial, o Google Fonts e os
        serviços de notificação) podem tratar dados em servidores fora do Brasil. Nesses casos, usamos
        fornecedores que oferecem garantias adequadas de proteção, como prevê o artigo 33 da LGPD.
      </p>
    ),
  },
  {
    id: 'prazos',
    title: 'Por quanto tempo guardamos',
    body: (
      <ul>
        <li>
          <strong>Histórico de posições e eventos do veículo</strong>: pelo prazo que você escolher ({history}{' '}
          dias; {LEGAL_FACTS.historyDefault} dias se você não escolher). O que passa do prazo é apagado
          automaticamente.
        </li>
        <li>
          <strong>Dados técnicos recebidos do rastreador</strong>: {LEGAL_FACTS.rawPacketsDays} dias, para
          diagnóstico.
        </li>
        <li>
          <strong>Contagem de visitas do site</strong>: {LEGAL_FACTS.analyticsDays} dias, sem identificar
          ninguém.
        </li>
        <li>
          <strong>Conta, veículos, assinaturas e faturas</strong>: enquanto a conta existir e, depois do
          encerramento, pelo tempo que a lei exigir (por exemplo, os registros fiscais).
        </li>
        <li>
          <strong>Registros de acesso</strong>: pelo menos 6 meses, como exige o Marco Civil da Internet, e
          pelo tempo necessário para a segurança do serviço e a defesa de direitos.
        </li>
        <li>
          <strong>Pré-cadastro e lista de pré-lançamento</strong>: enquanto forem úteis para o contato que você
          pediu, ou até você pedir a exclusão.
        </li>
      </ul>
    ),
  },
  {
    id: 'cookies',
    title: 'Cookies e dados guardados no seu aparelho',
    body: (
      <p>
        O site não usa cookies. Para você continuar conectado, o painel e o app guardam no seu aparelho os
        dados da sessão, a preferência de tema e, se você ativar a biometria, o identificador dela. O app
        instalado no celular também guarda os próprios arquivos para abrir mais rápido. Tudo isso é
        necessário para o funcionamento. Os dados da sessão são apagados quando você sai da conta, e o resto,
        quando você limpa os dados do navegador ou remove o app.
      </p>
    ),
  },
  {
    id: 'seguranca',
    title: 'Como protegemos os dados',
    body: (
      <ul>
        <li>Conexões cifradas (HTTPS) entre o seu aparelho e o nosso sistema.</li>
        <li>Senhas guardadas cifradas, e acesso de cada pessoa só ao que é dela.</li>
        <li>
          Bloqueio e desbloqueio do motor e a liberação de acesso a terceiros pedem a confirmação de que é
          você (biometria ou senha).
        </li>
        <li>As senhas de configuração do rastreador nunca aparecem no painel, no app nem nos registros.</li>
        <li>Registros das ações importantes, para investigar qualquer uso indevido.</li>
      </ul>
    ),
  },
  {
    id: 'direitos',
    title: 'Seus direitos',
    body: (
      <>
        <p>Pela LGPD, você pode pedir a qualquer momento:</p>
        <ul>
          <li>a confirmação de que tratamos os seus dados e o acesso a eles;</li>
          <li>a correção de dados incompletos, errados ou desatualizados;</li>
          <li>a anonimização, o bloqueio ou a exclusão de dados desnecessários ou tratados em desacordo com a lei;</li>
          <li>a portabilidade dos dados a outro fornecedor;</li>
          <li>a informação sobre com quem compartilhamos os seus dados;</li>
          <li>a exclusão dos dados tratados com o seu consentimento, e a retirada desse consentimento;</li>
          <li>a oposição a um tratamento que você considere irregular.</li>
        </ul>
        <p>
          Para isso, fale com a gente <ContactChannels />. Respondemos em até 15 dias. Se precisarmos manter
          algum dado por obrigação legal, explicamos qual e por quê. Você também pode reclamar à Autoridade
          Nacional de Proteção de Dados (ANPD).
        </p>
      </>
    ),
  },
  {
    id: 'pessoas',
    title: 'Rastreamos veículos, não pessoas',
    body: (
      <p>
        O serviço é para acompanhar e proteger veículos. Quem dirige o veículo também tem a localização
        registrada enquanto o usa: por isso, o cliente deve avisar as pessoas que usam o veículo com
        frequência. Usar o rastreador para vigiar alguém sem o conhecimento da pessoa é proibido pelos{' '}
        <Link to={TERMS_PATH}>Termos de Uso</Link> e pode ser crime.
      </p>
    ),
  },
  {
    id: 'menores',
    title: 'Crianças e adolescentes',
    body: <p>A contratação e o uso da conta são permitidos apenas a maiores de 18 anos.</p>,
  },
  {
    id: 'alteracoes',
    title: 'Mudanças nesta política',
    body: (
      <p>
        Podemos atualizar esta política para refletir mudanças no serviço ou na lei. A versão em vigor fica
        sempre nesta página, com a data no topo. Se a mudança for relevante, avisamos os clientes por e-mail
        antes de ela valer.
      </p>
    ),
  },
];

export function PrivacyPage() {
  return (
    <LegalLayout
      title="Política de Privacidade"
      intro={
        <p className={styles.callout}>
          Na {COMPANY.name}, os seus dados servem para uma coisa: proteger o seu veículo. Aqui está, em
          linguagem simples, o que tratamos, por quê, com quem compartilhamos e como você controla tudo isso.
        </p>
      }
      sections={SECTIONS}
    />
  );
}
