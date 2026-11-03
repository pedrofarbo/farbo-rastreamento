import { useEffect, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { checkoutApi } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { ARRANGE_DELIVERY, ShippingOptions } from '@/components/billing/ShippingOptions';
import { installmentChoices } from '@/components/billing/installments';
import { SelectField, TextField } from '@/components/ui/Field';
import { CONTRACT_PATH } from '@/config/legal';
import { CHECKOUT_ANCHOR } from '@/config/landing';
import { track, trackPageview } from '@/services/analytics';
import { checkoutTotals, payDestination, vehicleName } from '@/services/checkout';
import type { CheckoutChoice } from '@/services/checkout';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import { formatMoney, formatPhoneInput, isPhoneComplete } from '@/services/format';
import { KIND_NAMES, PRICED_KINDS, kindPrice } from '@/services/kindPrices';
import { currentReferral } from '@/services/referral';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';
import type { CheckoutOffer, DeliveryAddress, VehicleKind } from '@/types';

import pre from './PreLaunch.module.css';
import styles from './CheckoutPage.module.css';

const STEPS = ['Plano', 'Veículo', 'Seus dados', 'Pagamento'];

/** Os exemplos dos campos de marca e modelo, pelo tipo do veículo. */
const EXAMPLES: Record<VehicleKind, { brand: string; model: string }> = {
  MOTORCYCLE: { brand: 'Ex.: Honda', model: 'Ex.: CG 160' },
  CAR: { brand: 'Ex.: Fiat', model: 'Ex.: Uno' },
  TRUCK: { brand: 'Ex.: Volvo', model: 'Ex.: FH 540' },
};

interface Person {
  name: string;
  document: string;
  phone: string;
  email: string;
  password: string;
}

/**
 * A contratação rápida pela landing (/contratar): a pessoa física escolhe o
 * plano e o rastreador (comprado ou em comodato), informa o veículo, os dados
 * e o endereço, escolhe a entrega e paga com Pix na hora. A conta nasce
 * pronta: o Pix do pedido abre em seguida, já com a sessão do app.
 */
export function CheckoutPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { user, completeLogin } = useAuth();
  // O evento do QR Code de onde a pessoa veio (o convite do evento manda).
  const event = params.get('evento') ?? '';
  // Pelo link de um afiliado com tabela, a mensalidade é a do tipo do veículo
  // (do evento, sem o link guardado no aparelho, como na contratação).
  const { query: offer } = useCheckoutOffer(event ? '' : currentReferral());
  const [step, setStep] = useState(0);
  const [choice, setChoice] = useState<CheckoutChoice>({
    equipmentMode: 'sale',
    installments: 1,
    farboInstall: false,
  });
  const [kind, setKind] = useState<VehicleKind>('MOTORCYCLE');
  const [brand, setBrand] = useState('');
  const [model, setModel] = useState('');
  const [plate, setPlate] = useState('');
  const [person, setPerson] = useState<Person>({ name: '', document: '', phone: '', email: '', password: '' });
  const [address, setAddress] = useState<DeliveryAddress>(EMPTY_ADDRESS);
  // A entrega escolhida: o serviço do frete, ARRANGE_DELIVERY (combinar) ou 0 (nenhuma ainda).
  const [shipping, setShipping] = useState(0);
  const [accept, setAccept] = useState(false);
  const [website, setWebsite] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<ReactNode>('');

  useEffect(() => {
    document.title = 'Contratar · Farbo Rastreadores';
    trackPageview({ source: 'site', campaign: 'contratar' });
    track('checkout_open', event);
    // Só na abertura da página.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const o = offer.data;
  const addressReady = isAddressComplete(address);
  const quote = useQuery({
    queryKey: ['checkout-shipping', address.zipCode, address.city, address.state],
    queryFn: () => checkoutApi.shipping(address),
    enabled: step === 3 && addressReady && !!o,
    staleTime: 10 * 60_000,
  });
  const quotes = quote.data;
  const freightCents = quotes?.quotes.find((q) => q.serviceId === shipping)?.priceCents ?? 0;

  // A entrega volta a zero quando o CEP muda (as opções são outras).
  useEffect(() => setShipping(0), [address.zipCode]);
  // A única opção já vem marcada.
  useEffect(() => {
    if (!quotes || shipping !== 0) return;
    const options = [...quotes.quotes.map((q) => q.serviceId), ...(quotes.arrange ? [ARRANGE_DELIVERY] : [])];
    if (options.length === 1) setShipping(options[0]);
  }, [quotes, shipping]);

  const goTo = (next: number) => {
    setError('');
    setStep(next);
    window.scrollTo({ top: 0 });
  };

  if (offer.isLoading) {
    return (
      <Shell>
        <p className={pre.lead} role="status">
          Carregando…
        </p>
      </Shell>
    );
  }
  if (!o || !o.enabled) {
    return (
      <Shell>
        <section className={pre.intro}>
          <h1 className={pre.title}>{o ? 'A contratação pelo site está fechada agora' : 'Não deu para carregar agora'}</h1>
          <p className={pre.lead}>
            {o
              ? 'Deixe o seu contato que a gente fala com você e faz o seu pedido.'
              : 'Confira a sua internet e tente de novo em instantes.'}
          </p>
        </section>
        <div className={pre.actions}>
          {o ? (
            <Link className={pre.primary} to={`/${CHECKOUT_ANCHOR}`}>
              Deixar o meu contato
            </Link>
          ) : (
            <button type="button" className={pre.primary} onClick={() => void offer.refetch()}>
              Tentar de novo
            </button>
          )}
          <Link className={pre.secondary} to="/">
            Voltar ao site
          </Link>
        </div>
      </Shell>
    );
  }

  const totals = checkoutTotals(o, choice, freightCents, kind);
  const table = o.planByKind;
  const loan = choice.equipmentMode === 'loan' && o.loan !== null;
  const cpf = taxIdDigits(person.document);
  const personProblem =
    person.name.trim() === ''
      ? 'Informe o seu nome.'
      : cpf.length === 14
        ? 'A contratação pelo site é para pessoa física. Para empresa (CNPJ), fale com a gente.'
        : cpf.length !== 11 || !validTaxId(cpf)
          ? 'Confira o CPF.'
          : !isPhoneComplete(person.phone)
            ? 'Informe o celular com DDD.'
            : !/^\S+@\S+\.\S+$/.test(person.email.trim())
              ? 'Confira o e-mail.'
              : person.password.length < 8
                ? 'A senha precisa de pelo menos 8 caracteres.'
                : !addressReady
                  ? 'Complete o endereço de entrega.'
                  : '';
  const shippingReady = !o.shippingEnabled || shipping !== 0;
  const ready = accept && shippingReady && !quote.isFetching && !sending;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready) return;
    setError('');
    setSending(true);
    const email = person.email.trim();
    try {
      const result = await checkoutApi.place({
        equipmentMode: loan ? 'loan' : 'sale',
        installments: loan ? 1 : choice.installments,
        farboInstall: !loan && choice.farboInstall,
        vehicle: { kind, name: vehicleName(kind, brand, model), brand: brand.trim(), model: model.trim(), plate: plate.trim() },
        name: person.name.trim(),
        email,
        phone: person.phone,
        document: cpf,
        password: person.password,
        address,
        ...(shipping === ARRANGE_DELIVERY ? { arrangeDelivery: true } : shipping > 0 ? { shippingServiceId: shipping } : {}),
        contractVersion: o.contractVersion,
        accept,
        // Do QR Code de um evento, sem o link de indicação guardado no
        // aparelho: pode ser o tablet da equipe no estande.
        ref: event ? undefined : currentReferral() || undefined,
        event: event || undefined,
        website,
      });
      track('checkout_submit', loan ? 'comodato' : 'compra');
      if (result.tokens) {
        try {
          completeLogin(result.tokens, email);
        } catch {
          // Sem a sessão, o app pede o login (a conta já existe).
        }
      }
      // O Pix de todo o pedido; sem ele (nada a pagar agora), o app.
      const pay = result.payUrl ? payDestination(result.payUrl) : { url: '/app/' };
      if ('path' in pay) navigate(pay.path);
      else window.location.assign(pay.url);
    } catch (err) {
      setSending(false);
      setError(checkoutError(err));
    }
  };

  return (
    <Shell>
      {user && (
        <p className={styles.note}>
          Você já tem conta? Para mais um veículo, use o <a href="/app/">Novo veículo, no app</a>: o pedido já vai com os
          seus dados.
        </p>
      )}
      <section className={pre.intro}>
        <h1 className={pre.title}>Contrate em poucos minutos</h1>
        <p className={pre.lead}>Escolha o plano, informe o veículo e pague com Pix. O rastreador vai para o seu endereço.</p>
      </section>

      <ol className={styles.steps} aria-label="Etapas">
        {STEPS.map((label, i) => (
          <li
            key={label}
            className={`${styles.step} ${i === step ? styles.stepActive : i < step ? styles.stepDone : ''}`}
            aria-current={i === step ? 'step' : undefined}
          >
            {label}
          </li>
        ))}
      </ol>

      <form className={pre.form} onSubmit={submit} noValidate data-theme="dark">
        {step === 0 && (
          <>
            <fieldset className={styles.options}>
              <legend className={styles.legend}>{table ? 'O plano, pelo tipo do veículo' : 'O plano'}</legend>
              {table ? (
                PRICED_KINDS.map((k) => (
                  <Option
                    key={k}
                    checked={kind === k}
                    onChange={() => setKind(k)}
                    name="plan"
                    title={`${o.planName} · ${KIND_NAMES[k]}`}
                    price={`${formatMoney(kindPrice(table, k))}/mês`}
                  />
                ))
              ) : (
                <Option checked onChange={() => undefined} name="plan" title={o.planName} price={`${formatMoney(o.planCents)}/mês`}>
                  Rastreamento em tempo real, app, alertas, histórico de rotas e bloqueio pelo celular.
                </Option>
              )}
            </fieldset>
            <p className={styles.note}>
              {table && 'Rastreamento em tempo real, app, alertas, histórico de rotas e bloqueio pelo celular. '}O preço da
              mensalidade vale enquanto a assinatura durar (só o reajuste anual do contrato).
            </p>

            <fieldset className={styles.options}>
              <legend className={styles.legend}>O rastreador</legend>
              <Option
                checked={!loan}
                onChange={() => setChoice({ ...choice, equipmentMode: 'sale' })}
                name="equipment"
                title="Comprar o rastreador"
                price={formatMoney(o.equipmentCents)}
              >
                Ele é seu.{' '}
                {o.maxInstallments > 1
                  ? `À vista ou em até ${o.maxInstallments}x sem juros, somadas às mensalidades.`
                  : 'Pago à vista, no Pix.'}
              </Option>
              {o.loan && (
                <Option
                  checked={loan}
                  onChange={() => setChoice({ ...choice, equipmentMode: 'loan', installments: 1, farboInstall: false })}
                  name="equipment"
                  title="Comodato (emprestado)"
                  price={o.loan.surchargeCents > 0 ? `+${formatMoney(o.loan.surchargeCents)}/mês` : 'sem taxa mensal'}
                >
                  Sem pagar o rastreador: ele volta quando você encerrar.{' '}
                  {o.installDirect
                    ? `Instalação pelo representante por ${formatMoney(o.loan.installFeeCents)}, paga direto a ele`
                    : `Instalação pelos nossos instaladores por ${formatMoney(o.loan.installFeeCents)}`}
                  {o.loan.surchargeCents > 0 && ', com a garantia de troca do rastreador na mensalidade'}
                  {o.loan.uninstallFeeCents > 0 &&
                    `; desinstalação de ${formatMoney(o.loan.uninstallFeeCents)} ao encerrar${o.installDirect ? ', também direto a ele' : ''}`}
                  .
                </Option>
              )}
            </fieldset>

            {!loan && o.maxInstallments > 1 && (
              <div className={styles.panel}>
                <SelectField
                  label="Pagamento do rastreador"
                  value={choice.installments}
                  onChange={(e) => setChoice({ ...choice, installments: Number(e.target.value) })}
                >
                  {installmentChoices(o.equipmentCents, o.maxInstallments).map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </SelectField>
              </div>
            )}

            {!loan && (
              <fieldset className={styles.options}>
                <legend className={styles.legend}>A instalação</legend>
                <Option
                  checked={!choice.farboInstall}
                  onChange={() => setChoice({ ...choice, farboInstall: false })}
                  name="install"
                  title="Por conta própria"
                  price="sem custo"
                >
                  Com o instalador da sua preferência, pago direto a ele. O guia de instalação vai junto.
                </Option>
                {o.install && (
                  <Option
                    checked={choice.farboInstall}
                    onChange={() => setChoice({ ...choice, farboInstall: true })}
                    name="install"
                    title={o.installDirect ? 'Com o representante' : 'Com os instaladores da Farbo'}
                    price={formatMoney(o.install.priceCents)}
                  >
                    {o.installDirect
                      ? 'Pago direto a ele na instalação, fora do Pix. A gente agenda o dia com você.'
                      : 'A gente agenda o dia com você.'}
                  </Option>
                )}
              </fieldset>
            )}

            <Total totals={totals} shippingPending={o.shippingEnabled} />
            <button type="button" className={pre.submit} onClick={() => goTo(1)}>
              Continuar
            </button>
          </>
        )}

        {step === 1 && (
          <>
            <fieldset className={styles.options}>
              <legend className={styles.legend}>O veículo</legend>
              <div className={styles.kinds}>
                {PRICED_KINDS.map((k) => (
                  <Option key={k} checked={kind === k} onChange={() => setKind(k)} name="kind" title={KIND_NAMES[k]} />
                ))}
              </div>
            </fieldset>
            <div className={styles.panel}>
              <div className={styles.row}>
                <TextField label="Marca" autoCapitalize="words" maxLength={60} placeholder={EXAMPLES[kind].brand} value={brand} onChange={(e) => setBrand(e.target.value)} />
                <TextField label="Modelo" autoCapitalize="words" maxLength={60} placeholder={EXAMPLES[kind].model} value={model} onChange={(e) => setModel(e.target.value)} />
              </div>
              <TextField
                label="Placa"
                autoCapitalize="characters"
                maxLength={8}
                placeholder="ABC1D23"
                hint="Se ainda não tiver, deixe em branco."
                value={plate}
                onChange={(e) => setPlate(e.target.value.toUpperCase())}
              />
            </div>
            <div className={styles.actions}>
              <button type="button" className={pre.submit} disabled={!brand.trim() && !model.trim()} onClick={() => goTo(2)}>
                Continuar
              </button>
              <button type="button" className={pre.ghost} onClick={() => goTo(0)}>
                Voltar
              </button>
            </div>
          </>
        )}

        {step === 2 && (
          <>
            <div className={styles.panel}>
              <p className={styles.legend}>Seus dados</p>
              <TextField label="Nome completo" autoComplete="name" autoCapitalize="words" maxLength={120} value={person.name} onChange={(e) => setPerson({ ...person, name: e.target.value })} />
              <div className={styles.row}>
                <TextField
                  label="CPF"
                  inputMode="numeric"
                  maxLength={18}
                  placeholder="000.000.000-00"
                  value={formatTaxId(person.document)}
                  onChange={(e) => setPerson({ ...person, document: taxIdDigits(e.target.value).slice(0, 14) })}
                />
                <TextField
                  label="Celular (WhatsApp)"
                  type="tel"
                  inputMode="tel"
                  autoComplete="tel-national"
                  maxLength={16}
                  placeholder="(11) 99999-9999"
                  value={person.phone}
                  onChange={(e) => setPerson({ ...person, phone: formatPhoneInput(e.target.value, person.phone) })}
                />
              </div>
              <TextField
                label="E-mail"
                type="email"
                inputMode="email"
                autoComplete="email"
                autoCapitalize="none"
                maxLength={254}
                placeholder="voce@email.com"
                hint="É o seu login no app."
                value={person.email}
                onChange={(e) => setPerson({ ...person, email: e.target.value })}
              />
              <TextField
                label="Crie uma senha"
                type="password"
                autoComplete="new-password"
                minLength={8}
                hint="Pelo menos 8 caracteres."
                value={person.password}
                onChange={(e) => setPerson({ ...person, password: e.target.value })}
              />
            </div>
            <div className={styles.panel}>
              <p className={styles.legend}>Endereço de entrega</p>
              <AddressFields value={address} onChange={setAddress} />
            </div>
            {error && (
              <p className={pre.error} role="alert">
                {error}
              </p>
            )}
            <div className={styles.actions}>
              <button
                type="button"
                className={pre.submit}
                onClick={() => (personProblem ? setError(personProblem) : goTo(3))}
              >
                Continuar
              </button>
              <button type="button" className={pre.ghost} onClick={() => goTo(1)}>
                Voltar
              </button>
            </div>
          </>
        )}

        {step === 3 && (
          <>
            <div className={styles.panel}>
              <ShippingOptions
                view={quotes}
                loading={quote.isFetching}
                value={shipping}
                onChange={setShipping}
                onRetry={() => void quote.refetch()}
              />
            </div>

            <Summary
              offer={o}
              choice={choice}
              totals={totals}
              vehicle={vehicleName(kind, brand, model)}
              kind={kind}
              freightPending={!shippingReady}
            />

            {/* Isca para robôs: fora da tela e fora do Tab. */}
            <div className={styles.honeypot} aria-hidden="true">
              <label>
                Site
                <input type="text" name="website" tabIndex={-1} autoComplete="off" value={website} onChange={(e) => setWebsite(e.target.value)} />
              </label>
            </div>

            <label className={pre.consent}>
              <input type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
              <span>
                Li e aceito o{' '}
                <a href={CONTRACT_PATH} target="_blank" rel="noopener noreferrer">
                  contrato de prestação de serviço
                </a>{' '}
                (versão {o.contractVersion}).
              </span>
            </label>

            {error && (
              <p className={pre.error} role="alert">
                {error}
              </p>
            )}

            <div className={styles.actions}>
              <button type="submit" className={pre.submit} disabled={!ready}>
                {sending ? 'Preparando o Pix…' : `Pagar ${formatMoney(totals.totalNowCents)} com Pix`}
              </button>
              <button type="button" className={pre.ghost} onClick={() => goTo(2)} disabled={sending}>
                Voltar
              </button>
            </div>
            <p className={pre.small}>
              A sua conta no app é criada agora; o Pix abre em seguida e vale para todo o pedido.
            </p>
          </>
        )}
      </form>
    </Shell>
  );
}

/** O erro da contratação, como a pessoa entende (o e-mail com conta leva ao login). */
function checkoutError(err: unknown): ReactNode {
  if (err instanceof ApiError) {
    if ((err.body as { code?: string } | undefined)?.code === 'EMAIL_TAKEN') {
      return (
        <>
          Este e-mail já tem conta. <Link to="/login">Entre no app</Link> e use o Novo veículo (ou{' '}
          <Link to="/esqueci-senha">Esqueci a senha</Link>).
        </>
      );
    }
    if (err.status === 429) return 'Muita gente contratando agora. Espere uns segundos e toque de novo.';
  }
  return err instanceof Error && err.message ? err.message : 'Não deu para concluir agora. Tente de novo.';
}

function Shell({ children }: { children: ReactNode }) {
  return (
    <main className={pre.page}>
      <header className={pre.header}>
        <Link to="/">
          <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={pre.logo} />
        </Link>
        <span className={pre.badge}>Contratação</span>
      </header>
      {children}
    </main>
  );
}

function Option({
  checked,
  onChange,
  name,
  title,
  price,
  children,
}: {
  checked: boolean;
  onChange: () => void;
  name: string;
  title: string;
  price?: string;
  children?: ReactNode;
}) {
  return (
    <label className={`${styles.option} ${checked ? styles.optionOn : ''}`}>
      <input type="radio" name={name} checked={checked} onChange={onChange} />
      <span className={styles.optionBody}>
        <span className={styles.optionTitle}>
          <span>{title}</span>
          {price && <span className={styles.optionPrice}>{price}</span>}
        </span>
        {children && <span className={styles.optionText}>{children}</span>}
      </span>
    </label>
  );
}

/** O total de agora, no pé das etapas antes do resumo. */
function Total({ totals, shippingPending }: { totals: ReturnType<typeof checkoutTotals>; shippingPending: boolean }) {
  return (
    <div className={styles.summary}>
      <div className={styles.line}>
        <span>Mensalidade</span>
        <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span>
      </div>
      <div className={`${styles.line} ${styles.total}`}>
        <span>Hoje, no Pix</span>
        <span>
          {formatMoney(totals.totalNowCents)}
          {shippingPending ? ' + frete' : ''}
        </span>
      </div>
    </div>
  );
}

/** O resumo do pedido: o que se paga agora e a mensalidade. */
function Summary({
  offer,
  choice,
  totals,
  vehicle,
  kind,
  freightPending,
}: {
  offer: CheckoutOffer;
  choice: CheckoutChoice;
  totals: ReturnType<typeof checkoutTotals>;
  vehicle: string;
  kind: VehicleKind;
  freightPending: boolean;
}) {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  const plan = offer.planByKind ? `${offer.planName} · ${KIND_NAMES[kind]}` : offer.planName;
  // Pelo link de um representante: a instalação e a desinstalação são pagas direto a ele.
  const directInstall = offer.installDirect
    ? loan
      ? (offer.loan?.installFeeCents ?? 0)
      : choice.farboInstall
        ? (offer.install?.priceCents ?? 0)
        : 0
    : 0;
  const parcels = totals.parcels.length;
  return (
    <div className={styles.summary} aria-label="Resumo do pedido">
      <p className={styles.legend}>Resumo · {vehicle}</p>
      <div className={styles.line}>
        <span>1ª mensalidade · {plan}</span>
        <span>{formatMoney(totals.monthlyCents)}</span>
      </div>
      {parcels > 0 && (
        <div className={styles.line}>
          <span>Rastreador, 1ª de {parcels} parcelas</span>
          <span>{formatMoney(totals.parcels[0])}</span>
        </div>
      )}
      {totals.equipmentNowCents > 0 && (
        <div className={styles.line}>
          <span>Rastreador, à vista</span>
          <span>{formatMoney(totals.equipmentNowCents)}</span>
        </div>
      )}
      {totals.installNowCents > 0 && (
        <div className={styles.line}>
          <span>Instalação pelos instaladores da Farbo</span>
          <span>{formatMoney(totals.installNowCents)}</span>
        </div>
      )}
      {totals.freightCents > 0 && (
        <div className={styles.line}>
          <span>Frete</span>
          <span>{formatMoney(totals.freightCents)}</span>
        </div>
      )}
      <div className={`${styles.line} ${styles.total}`}>
        <span>Hoje, no Pix</span>
        <span>{freightPending ? '—' : formatMoney(totals.totalNowCents)}</span>
      </div>
      <p className={styles.note}>
        Depois, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span> no dia {offer.dueDay}
        {parcels > 1 && `, com as outras ${parcels - 1} parcelas do rastreador somadas às próximas mensalidades`}
        {loan && totals.guaranteeCents > 0 && `; ${formatMoney(totals.guaranteeCents)} dela é a garantia de troca do rastreador`}
        {loan &&
          offer.loan &&
          offer.loan.uninstallFeeCents > 0 &&
          `. Ao encerrar, a desinstalação custa ${formatMoney(offer.loan.uninstallFeeCents)}${offer.installDirect ? ', paga direto ao representante' : ''}`}
        .
      </p>
      {directInstall > 0 && (
        <p className={styles.note}>
          A instalação ({formatMoney(directInstall)}) é paga direto ao representante, no dia, fora do Pix.
        </p>
      )}
    </div>
  );
}
