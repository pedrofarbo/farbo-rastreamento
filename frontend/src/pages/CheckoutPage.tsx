import { useEffect, useState } from 'react';
import type { FormEvent, KeyboardEvent, ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { checkoutApi } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { ARRANGE_DELIVERY, ShippingOptions } from '@/components/billing/ShippingOptions';
import { TextField } from '@/components/ui/Field';
import { CONTRACT_PATH } from '@/config/legal';
import { CHECKOUT_ANCHOR, trackerAdded } from '@/config/landing';
import { track, trackPageview } from '@/services/analytics';
import {
  blockingOffered,
  checkoutTotals,
  chosenInstall,
  chosenPeriod,
  couponLabel,
  payDestination,
  periodPrice,
  vehicleName,
} from '@/services/checkout';
import type { CheckoutChoice } from '@/services/checkout';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import { formatDateOnly, formatMoney, formatPhoneInput, isPhoneComplete } from '@/services/format';
import { KIND_NAMES, PRICED_KINDS, kindPrice } from '@/services/kindPrices';
import { currentReferral } from '@/services/referral';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';
import type { CheckoutLinkOffer, CheckoutOffer, CouponOffer, DeliveryAddress, VehicleKind } from '@/types';

import pre from './PreLaunch.module.css';
import styles from './CheckoutPage.module.css';

const STEPS = ['Plano', 'Veículo', 'Seus dados', 'Pagamento'];

/** Os exemplos dos campos de marca e modelo, pelo tipo do veículo. */
const EXAMPLES: Record<VehicleKind, { brand: string; model: string }> = {
  MOTORCYCLE: { brand: 'Ex.: Honda', model: 'Ex.: CG 160' },
  CAR: { brand: 'Ex.: Fiat', model: 'Ex.: Uno' },
  TRUCK: { brand: 'Ex.: Volvo', model: 'Ex.: FH 540' },
};

/** A escolha que o link de pagamento já traz: a oferta fechada dele. */
export function linkChoice(link: CheckoutLinkOffer): CheckoutChoice {
  return {
    equipmentMode: link.equipmentMode,
    farboInstall: link.equipmentMode === 'sale' && link.farboInstall,
    ...(link.periodMonths > 1 ? { periodMonths: link.periodMonths } : {}),
    installBlocking: link.installBlocking,
  };
}

interface Person {
  name: string;
  document: string;
  phone: string;
  email: string;
  password: string;
}

/**
 * A contratação rápida pela landing (/contratar): a pessoa física escolhe o
 * plano e o rastreador (incluso na mensalidade, que fica com ela, ou em
 * comodato), informa o veículo, os dados e o endereço, escolhe a entrega e
 * paga com Pix na hora. A Farbo não vende o rastreador. A conta nasce
 * pronta: o Pix do pedido abre em seguida, já com a sessão do app.
 */
export function CheckoutPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { user, completeLogin } = useAuth();
  // O evento do QR Code de onde a pessoa veio (o convite do evento manda).
  const event = params.get('evento') ?? '';
  // O link de pagamento da central (?link=): a oferta fechada dele, sem o representante.
  const linkToken = (params.get('link') ?? '').trim();
  // Pelo link de um afiliado com tabela, a mensalidade é a do tipo do veículo
  // (do evento, sem o link guardado no aparelho, como na contratação).
  const ref = event || linkToken ? '' : currentReferral();
  const { query: offer } = useCheckoutOffer(ref, linkToken);
  const [step, setStep] = useState(0);
  const [choice, setChoice] = useState<CheckoutChoice>({ equipmentMode: 'sale', farboInstall: false });
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
  // O cupom de desconto (só na contratação direta): o aplicado e o do link (?cupom=), que já vem conferido.
  const [coupon, setCoupon] = useState<CouponOffer | null>(null);
  const linkCoupon = params.get('cupom') ?? '';

  useEffect(() => {
    document.title = 'Contratar · Farbo Rastreadores';
    trackPageview({ source: 'site', campaign: 'contratar' });
    track('checkout_open', event);
    // Só na abertura da página.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const o = offer.data;
  // O cupom do link: conferido assim que a oferta diz que há cupom valendo.
  useEffect(() => {
    if (!o?.coupons || !linkCoupon.trim()) return;
    let live = true;
    checkoutApi.coupon(linkCoupon).then(
      (c) => live && setCoupon(c),
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [o?.coupons, linkCoupon]);
  const addressReady = isAddressComplete(address);
  const quote = useQuery({
    queryKey: ['checkout-shipping', address.zipCode, address.city, address.state],
    // Pelo link do representante que entrega em mãos, só a entrega dele.
    queryFn: () => checkoutApi.shipping(address, ref),
    // Pelo link de pagamento com a entrega em mãos, não há frete a cotar.
    enabled: step === 3 && addressReady && !!o && !o.link?.handDelivery,
    staleTime: 10 * 60_000,
  });
  const quotes = quote.data;
  const freightCents = quotes?.quotes.find((q) => q.serviceId === shipping)?.priceCents ?? 0;

  // Pelo link de um afiliado só de comodato, o comodato já vem marcado.
  useEffect(() => {
    if (o?.equipmentModes === 'loan' && choice.equipmentMode !== 'loan') {
      setChoice((c) => ({ ...c, equipmentMode: 'loan', farboInstall: false }));
    }
  }, [o?.equipmentModes, choice.equipmentMode]);
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
  if (o && linkToken && !o.link) {
    return (
      <Shell>
        <section className={pre.intro}>
          <h1 className={pre.title}>Link de pagamento indisponível</h1>
          <p className={pre.lead}>
            {o.linkProblem ? `${o.linkProblem.charAt(0).toUpperCase()}${o.linkProblem.slice(1)}.` : 'Confira o endereço.'} Fale com
            a gente para receber um novo{o.enabled ? ' ou veja os planos do site' : ''}.
          </p>
        </section>
        <div className={pre.actions}>
          <Link className={pre.primary} to={`/${CHECKOUT_ANCHOR}`}>
            Deixar o meu contato
          </Link>
          {o.enabled && (
            <Link className={pre.secondary} to="/contratar">
              Ver os planos do site
            </Link>
          )}
        </div>
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

  const applied = o.coupons ? coupon : null;
  // Pelo link de pagamento, a escolha é a dele (sem escolher nada).
  const current = o.link ? linkChoice(o.link) : choice;
  const totals = checkoutTotals(o, current, freightCents, kind, applied);
  const table = o.planByKind;
  const loan = current.equipmentMode === 'loan' && o.loan !== null;
  // O semestral e o anual (com o rastreador incluso ou em comodato), quando o link os oferece.
  const periods = o.periods ?? [];
  const period = chosenPeriod(o, current);
  // Pela tabela do afiliado, a parte do rastreador incluso soma nas primeiras.
  const added = trackerAdded(o);
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
  // Pelo link de pagamento com a entrega em mãos pela Farbo: sem frete nem escolha.
  const inPerson = Boolean(o.link?.handDelivery);
  const shippingReady = !o.shippingEnabled || inPerson || shipping !== 0;
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
        ...(period ? { periodMonths: period.months } : {}),
        farboInstall: !loan && current.farboInstall,
        ...(chosenInstall(o, current).blocking ? { installBlocking: true } : {}),
        ...(applied ? { coupon: applied.code } : {}),
        ...(o.link ? { link: o.link.token } : {}),
        vehicle: { kind, name: vehicleName(kind, brand, model), brand: brand.trim(), model: model.trim(), plate: plate.trim() },
        name: person.name.trim(),
        email,
        phone: person.phone,
        document: cpf,
        password: person.password,
        address,
        ...(inPerson ? {} : shipping === ARRANGE_DELIVERY ? { arrangeDelivery: true } : shipping > 0 ? { shippingServiceId: shipping } : {}),
        contractVersion: o.contractVersion,
        accept,
        // Do QR Code de um evento, sem o link de indicação guardado no
        // aparelho: pode ser o tablet da equipe no estande. Pelo link de
        // pagamento, a oferta é a dele, mas a conta fica com o representante.
        ref: event ? undefined : currentReferral() || undefined,
        event: event || undefined,
        website,
      });
      track('checkout_submit', loan ? 'comodato' : 'incluso');
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
        {o.link ? (
          <>
            <h1 className={pre.title}>A sua proposta</h1>
            <p className={pre.lead}>
              As condições abaixo foram preparadas para você. Informe o veículo e os seus dados e pague com{' '}
              {o.card ? 'Pix, boleto ou cartão de crédito' : 'Pix'}.
            </p>
          </>
        ) : (
          <>
            <h1 className={pre.title}>Contrate em poucos minutos</h1>
            <p className={pre.lead}>
              Escolha o plano, informe o veículo e pague com {o.card ? 'Pix, boleto ou cartão de crédito' : 'Pix'}. O
              rastreador vem incluso e vai para o seu endereço.
            </p>
          </>
        )}
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
        {step === 0 && o.link && (
          <>
            <LinkOffer link={o.link} offer={o} />
            <Total totals={totals} shippingPending={o.shippingEnabled && !o.link.handDelivery} card={o.card} />
            <button type="button" className={pre.submit} onClick={() => goTo(1)}>
              Continuar
            </button>
          </>
        )}
        {step === 0 && !o.link && (
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
                  >
                    {added && !loan && `+ ${formatMoney(added.cents)}/mês nas ${added.months} primeiras mensalidades, com o rastreador incluso.`}
                  </Option>
                ))
              ) : (
                <Option checked onChange={() => undefined} name="plan" title={o.planName} price={`${formatMoney(o.planCents)}/mês`}>
                  Rastreamento em tempo real, app, alertas, histórico de rotas e bloqueio pelo celular.
                </Option>
              )}
            </fieldset>
            <p className={styles.note}>
              {table && 'Rastreamento em tempo real, app, alertas, histórico de rotas e bloqueio pelo celular. '}O preço da
              mensalidade vale enquanto a assinatura durar (só o reajuste anual do contrato)
              {!loan && (added ? ', e a parte do rastreador incluso sai nas primeiras.' : ', com o rastreador incluso.')}
              {loan && '.'}
            </p>

            {periods.length > 0 && (
              <fieldset className={styles.options}>
                <legend className={styles.legend}>Como pagar</legend>
                <Option
                  checked={!period}
                  onChange={() => setChoice({ ...choice, periodMonths: 1 })}
                  name="period"
                  title="Mensal"
                  price={`${formatMoney(table ? kindPrice(table, kind) : o.planCents)}/mês`}
                >
                  Uma fatura por mês.
                </Option>
                {periods.map((p) => {
                  const cents = periodPrice(p, loan);
                  return (
                    <Option
                      key={p.months}
                      checked={period?.months === p.months}
                      onChange={() => setChoice({ ...choice, periodMonths: p.months })}
                      name="period"
                      title={p.months === 12 ? 'Anual' : 'Semestral'}
                      price={`${formatMoney(cents)}${p.months === 12 ? '/ano' : '/semestre'}`}
                    >
                      {formatMoney(Math.floor(cents / p.months))} por mês, pagos de uma vez a cada {p.months} meses,{' '}
                      {loan
                        ? cents > p.cents
                          ? 'com a garantia de troca do rastreador emprestado.'
                          : 'com o rastreador emprestado.'
                        : 'com o rastreador incluso no valor.'}{' '}
                      Se cancelar antes do fim do período pago, não há devolução: o rastreamento segue até o fim dele e não
                      renova.
                    </Option>
                  );
                })}
              </fieldset>
            )}

            <fieldset className={styles.options}>
              <legend className={styles.legend}>O rastreador</legend>
              {o.equipmentModes !== 'loan' && (
                <Option
                  checked={!loan}
                  onChange={() => setChoice({ ...choice, equipmentMode: 'sale' })}
                  name="equipment"
                  title="Rastreador incluso"
                  price={period ? 'no valor do plano' : added ? `+${formatMoney(added.cents)}/mês nas ${added.months} primeiras` : 'na mensalidade'}
                >
                  Ele fica com você, e você só paga o frete.
                  {!period && o.tracker.cents > 0 && o.tracker.months > 0 && (
                    <>
                      {' '}
                      Se encerrar antes da {o.tracker.months}ª mensalidade, a parte do rastreador que falta (
                      {formatMoney(o.tracker.cents)} por mensalidade) vence de uma vez.
                    </>
                  )}
                </Option>
              )}
              {o.loan && (
                <Option
                  checked={loan}
                  onChange={() => setChoice({ ...choice, equipmentMode: 'loan', farboInstall: false })}
                  name="equipment"
                  title="Comodato (emprestado)"
                  price={
                    o.loan.surchargeCents <= 0
                      ? 'sem taxa mensal'
                      : period
                        ? `+${formatMoney(o.loan.surchargeCents * period.months)} no ${period.months === 12 ? 'ano' : 'semestre'}`
                        : `+${formatMoney(o.loan.surchargeCents)}/mês`
                  }
                >
                  O rastreador emprestado: ele volta quando você encerrar.{' '}
                  {blockingOffered(o)
                    ? `Instalação ${o.installDirect ? 'pelo representante' : 'pelos nossos instaladores'} sem ou com bloqueio (escolha abaixo)${o.installDirect ? ', paga direto a ele' : ''}`
                    : o.installDirect
                      ? o.loan.installFeeCents > 0
                        ? `Instalação pelo representante por ${formatMoney(o.loan.installFeeCents)}, paga direto a ele`
                        : 'Instalação pelo representante, sem custo'
                      : `Instalação pelos nossos instaladores por ${formatMoney(o.loan.installFeeCents)}`}
                  {o.loan.surchargeCents > 0 && ', com a garantia de troca do rastreador na mensalidade'}
                  {o.loan.uninstallFeeCents > 0 &&
                    `; desinstalação de ${formatMoney(o.loan.uninstallFeeCents)} ao encerrar${o.installDirect ? ', também direto a ele' : ''}`}
                  .
                </Option>
              )}
            </fieldset>

            {!loan && (
              <fieldset className={styles.options}>
                <legend className={styles.legend}>A instalação</legend>
                <Option
                  checked={!choice.farboInstall}
                  onChange={() => setChoice({ ...choice, farboInstall: false, installBlocking: false })}
                  name="install"
                  title="Por conta própria"
                  price="sem custo"
                >
                  Com o instalador da sua preferência, pago direto a ele. O guia de instalação vai junto.
                </Option>
                {o.install && !blockingOffered(o) && (
                  <Option
                    checked={choice.farboInstall}
                    onChange={() => setChoice({ ...choice, farboInstall: true, installBlocking: false })}
                    name="install"
                    title={o.installDirect ? 'Com o representante' : 'Com os instaladores da Farbo'}
                    price={o.install.priceCents > 0 ? formatMoney(o.install.priceCents) : 'sem custo'}
                  >
                    {o.installDirect
                      ? 'Pago direto a ele na instalação, fora do Pix. A gente agenda o dia com você.'
                      : 'A gente agenda o dia com você.'}
                  </Option>
                )}
                {o.install && blockingOffered(o) && (
                  <BlockingOptions offer={o} choice={choice} setChoice={setChoice} sale />
                )}
              </fieldset>
            )}
            {loan && blockingOffered(o) && (
              <fieldset className={styles.options}>
                <legend className={styles.legend}>A instalação</legend>
                <BlockingOptions offer={o} choice={choice} setChoice={setChoice} />
              </fieldset>
            )}

            <Total totals={totals} shippingPending={o.shippingEnabled && !o.handDelivery} card={o.card} />
            <button type="button" className={pre.submit} onClick={() => goTo(1)}>
              Continuar
            </button>
          </>
        )}

        {step === 1 && (
          <>
            <fieldset className={styles.options}>
              <legend className={styles.legend}>O veículo</legend>
              {table ? (
                // Pelo link com a tabela por tipo, o tipo é o do plano escolhido:
                // trocar aqui mudaria a mensalidade sem voltar ao plano.
                <p className={styles.note}>
                  {KIND_NAMES[kind]}, o tipo do plano escolhido.{' '}
                  <button type="button" className={styles.linkButton} onClick={() => goTo(0)}>
                    Mudar o plano
                  </button>
                </p>
              ) : (
                <div className={styles.kinds}>
                  {PRICED_KINDS.map((k) => (
                    <Option key={k} checked={kind === k} onChange={() => setKind(k)} name="kind" title={KIND_NAMES[k]} />
                  ))}
                </div>
              )}
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
              {inPerson ? (
                <>
                  <p className={styles.legend}>Entrega</p>
                  <p className={styles.note}>Em mãos: a Farbo entrega o rastreador e combina o dia com você. Sem frete.</p>
                </>
              ) : (
                <ShippingOptions
                  view={quotes}
                  loading={quote.isFetching}
                  value={shipping}
                  onChange={setShipping}
                  onRetry={() => void quote.refetch()}
                />
              )}
            </div>

            {o.coupons && (
              <CouponField key={applied?.code ?? ''} initial={linkCoupon} applied={applied} onChange={setCoupon} disabled={sending} />
            )}

            <Summary
              offer={o}
              choice={current}
              totals={totals}
              vehicle={vehicleName(kind, brand, model)}
              kind={kind}
              freightPending={!shippingReady}
              coupon={applied}
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
                {sending
                  ? o.card
                    ? 'Preparando o pagamento…'
                    : 'Preparando o Pix…'
                  : o.card
                    ? `Pagar ${formatMoney(totals.totalNowCents)}`
                    : `Pagar ${formatMoney(totals.totalNowCents)} com Pix`}
              </button>
              <button type="button" className={pre.ghost} onClick={() => goTo(2)} disabled={sending}>
                Voltar
              </button>
            </div>
            <p className={pre.small}>
              {o.card
                ? 'Em seguida, você escolhe Pix, boleto ou cartão de crédito, para todo o pedido. A contratação só fica valendo com o pagamento: se ele não for feito até o vencimento, o cadastro é cancelado (e dá para contratar de novo).'
                : 'O Pix abre em seguida e vale para todo o pedido. A contratação só fica valendo com o pagamento: se o Pix vencer sem ser pago, o cadastro é cancelado (e dá para contratar de novo).'}
            </p>
          </>
        )}
      </form>
    </Shell>
  );
}

/**
 * A oferta fechada do link de pagamento, sem escolha: o plano e o valor, o rastreador, quem
 * instala (com ou sem bloqueio) e as taxas, e até quando vale.
 */
function LinkOffer({ link, offer }: { link: CheckoutLinkOffer; offer: CheckoutOffer }) {
  const loan = link.equipmentMode === 'loan';
  const period = link.periodMonths > 1 ? link.periodMonths : 0;
  const plan = period ? (period === 12 ? 'Plano anual' : 'Plano semestral') : 'Plano mensal';
  const installed = loan || link.farboInstall;
  const tracker = offer.tracker;
  return (
    <div className={styles.summary} aria-label="A sua oferta">
      <p className={styles.legend}>A sua oferta</p>
      <div className={styles.line}>
        <span>{plan}</span>
        <span className={styles.monthly}>
          {formatMoney(link.priceCents)}
          {period === 12 ? '/ano' : period === 6 ? '/semestre' : '/mês'}
        </span>
      </div>
      <div className={styles.line}>
        <span>{loan ? 'Rastreador em comodato (emprestado)' : 'Rastreador incluso (fica com você)'}</span>
        <span>{loan ? 'volta ao encerrar' : 'incluso'}</span>
      </div>
      <div className={styles.line}>
        <span>
          {installed
            ? `Instalação pelos instaladores da Farbo, ${link.installBlocking ? 'com' : 'sem'} bloqueio`
            : 'Instalação por conta própria'}
        </span>
        <span>
          {installed && link.installCents > 0
            ? `${formatMoney(link.installCents)}${link.installDirect ? ' · direto ao instalador' : ''}`
            : 'sem custo'}
        </span>
      </div>
      {loan && link.uninstallCents > 0 && (
        <div className={styles.line}>
          <span>Desinstalação, ao encerrar</span>
          <span>{formatMoney(link.uninstallCents)}</span>
        </div>
      )}
      {link.handDelivery && (
        <div className={styles.line}>
          <span>Entrega em mãos pela Farbo</span>
          <span>sem frete</span>
        </div>
      )}
      <p className={styles.note}>
        Rastreamento em tempo real, app, alertas, histórico de rotas e bloqueio pelo celular
        {link.installBlocking && installed ? ' (o relé do bloqueio vai na instalação)' : ''}.{' '}
        {period
          ? `${formatMoney(Math.floor(link.priceCents / period))} por mês, pagos de uma vez a cada ${period} meses. Se cancelar antes do fim do período pago, não há devolução: o rastreamento segue até o fim dele e não renova.`
          : 'O valor vale enquanto a assinatura durar (só o reajuste anual do contrato).'}
        {!installed && ' Com o instalador da sua preferência, pago direto a ele; o guia de instalação vai junto.'}
        {installed && ' A gente agenda o dia com você.'}
        {installed && link.installDirect && link.installCents > 0 && ' A instalação é paga direto ao instalador, no dia, fora do Pix.'}
      </p>
      {!loan && !period && tracker.cents > 0 && tracker.months > 0 && (
        <p className={styles.note}>
          Se a assinatura for encerrada antes da {tracker.months}ª mensalidade, a parte do rastreador que falta (
          {formatMoney(tracker.cents)} por mensalidade) vence de uma vez.
        </p>
      )}
      {link.validUntil && <p className={styles.note}>Condições válidas para contratar até {formatDateOnly(link.validUntil)}.</p>}
    </div>
  );
}

/**
 * O erro da contratação, como a pessoa entende: o e-mail com conta leva ao
 * login; o de uma contratação ainda esperando o pagamento, ao pedido já
 * feito (o link vem só com a senha dela).
 */
function checkoutError(err: unknown): ReactNode {
  if (err instanceof ApiError) {
    const body = err.body as { code?: string; payUrl?: string } | undefined;
    if (body?.code === 'CHECKOUT_UNPAID') {
      return (
        <>
          {`${err.message.charAt(0).toUpperCase()}${err.message.slice(1)}.`}
          {body.payUrl && (
            <>
              {' '}
              <a href={body.payUrl}>Pagar o pedido já feito</a>
            </>
          )}
        </>
      );
    }
    if (body?.code === 'EMAIL_TAKEN') {
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

/**
 * A instalação sem ou com o relé do bloqueio do motor, quando o link oferece a escolha: no
 * rastreador incluso (sale), ao lado da instalação por conta própria; no comodato, sozinhas.
 */
function BlockingOptions({
  offer,
  choice,
  setChoice,
  sale = false,
}: {
  offer: CheckoutOffer;
  choice: CheckoutChoice;
  setChoice: (c: CheckoutChoice) => void;
  sale?: boolean;
}) {
  const who = offer.installDirect ? 'Com o representante' : 'Com os instaladores da Farbo';
  const plain = sale ? (offer.install?.priceCents ?? 0) : (offer.loan?.installFeeCents ?? 0);
  const blocking = offer.install?.blockingCents ?? 0;
  const on = !sale || choice.farboInstall;
  const paid = offer.installDirect ? ' Pago direto a ele na instalação, fora do Pix.' : '';
  return (
    <>
      <Option
        checked={on && !choice.installBlocking}
        onChange={() => setChoice({ ...choice, farboInstall: sale, installBlocking: false })}
        name="install"
        title={`${who}, sem bloqueio`}
        price={plain > 0 ? formatMoney(plain) : 'sem custo'}
      >
        O rastreador com a localização, os trajetos e os alertas.{paid} A gente agenda o dia com você.
      </Option>
      <Option
        checked={on && Boolean(choice.installBlocking)}
        onChange={() => setChoice({ ...choice, farboInstall: sale, installBlocking: true })}
        name="install"
        title={`${who}, com bloqueio`}
        price={formatMoney(blocking)}
      >
        Também o relé que bloqueia o motor pelo app (com o veículo parado).{paid} A gente agenda o dia com você.
      </Option>
    </>
  );
}

/**
 * O cupom de desconto (só na contratação direta): a pessoa digita e aplica (o servidor confere e diz o
 * desconto, ou o motivo de não valer); aplicado, o desconto e o "Remover". O Enter aplica, sem enviar o
 * pedido. O do link (?cupom=) já vem no campo.
 */
function CouponField({
  initial,
  applied,
  onChange,
  disabled,
}: {
  initial: string;
  applied: CouponOffer | null;
  onChange: (coupon: CouponOffer | null) => void;
  disabled: boolean;
}) {
  const [code, setCode] = useState(applied?.code ?? initial);
  const [checking, setChecking] = useState(false);
  const [problem, setProblem] = useState('');
  const apply = async () => {
    if (!code.trim() || checking) return;
    setChecking(true);
    setProblem('');
    try {
      onChange(await checkoutApi.coupon(code.trim()));
    } catch (err) {
      setProblem(
        err instanceof ApiError && err.status === 429
          ? 'Muitas tentativas. Espere um minuto e tente de novo.'
          : err instanceof Error && err.message
            ? `${err.message.charAt(0).toUpperCase()}${err.message.slice(1)}.`
            : 'Não deu para conferir o cupom agora. Tente de novo.',
      );
    } finally {
      setChecking(false);
    }
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    void apply();
  };
  if (applied) {
    return (
      <div className={styles.couponApplied} role="status">
        <span>
          Cupom <strong>{applied.code}</strong>: {couponLabel(applied)}.
        </span>
        <button type="button" className={styles.linkButton} onClick={() => onChange(null)} disabled={disabled}>
          Remover
        </button>
      </div>
    );
  }
  return (
    <div className={`${styles.panel} ${styles.couponRow}`}>
      <TextField
        label="Cupom de desconto"
        autoCapitalize="characters"
        autoComplete="off"
        spellCheck={false}
        maxLength={30}
        placeholder="Se tiver um, digite aqui"
        value={code}
        error={problem || undefined}
        onChange={(e) => {
          setCode(e.target.value.toUpperCase());
          setProblem('');
        }}
        onKeyDown={onKeyDown}
      />
      <button type="button" className={styles.couponApply} onClick={() => void apply()} disabled={!code.trim() || checking || disabled}>
        {checking ? 'Conferindo…' : 'Aplicar'}
      </button>
    </div>
  );
}

/** O desconto do cupom aplicado, na linha do resumo. */
function CouponLine({ totals }: { totals: ReturnType<typeof checkoutTotals> }) {
  if (totals.couponCents <= 0) return null;
  return (
    <div className={`${styles.line} ${styles.discount}`}>
      <span>Cupom de desconto</span>
      <span>−{formatMoney(totals.couponCents)}</span>
    </div>
  );
}

/** O total de agora, no pé das etapas antes do resumo. */
function Total({
  totals,
  shippingPending,
  card = false,
}: {
  totals: ReturnType<typeof checkoutTotals>;
  shippingPending: boolean;
  /** Com o cartão ligado, o pagamento de hoje não é só Pix. */
  card?: boolean;
}) {
  const today = card ? 'Hoje' : 'Hoje, no Pix';
  if (totals.periodMonths > 1) {
    return (
      <div className={styles.summary}>
        <div className={styles.line}>
          <span>{totals.periodMonths === 12 ? 'Plano anual' : 'Plano semestral'}</span>
          <span className={styles.monthly}>
            {formatMoney(totals.monthlyCents)}
            {totals.periodMonths === 12 ? '/ano' : '/semestre'}
          </span>
        </div>
        <CouponLine totals={totals} />
        <div className={`${styles.line} ${styles.total}`}>
          <span>{today}</span>
          <span>
            {formatMoney(totals.totalNowCents)}
            {shippingPending ? ' + frete' : ''}
          </span>
        </div>
      </div>
    );
  }
  return (
    <div className={styles.summary}>
      <div className={styles.line}>
        <span>{totals.trackerMonths > 0 ? `Mensalidade, nas ${totals.trackerMonths} primeiras` : 'Mensalidade'}</span>
        <span className={styles.monthly}>{formatMoney(totals.firstMonthlyCents + totals.couponCents)}/mês</span>
      </div>
      <CouponLine totals={totals} />
      {totals.trackerMonths > 0 && (
        <div className={styles.line}>
          <span>Depois</span>
          <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span>
        </div>
      )}
      <div className={`${styles.line} ${styles.total}`}>
        <span>{today}</span>
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
  coupon,
}: {
  offer: CheckoutOffer;
  choice: CheckoutChoice;
  totals: ReturnType<typeof checkoutTotals>;
  vehicle: string;
  kind: VehicleKind;
  freightPending: boolean;
  coupon: CouponOffer | null;
}) {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  const period = chosenPeriod(offer, choice);
  const plan = period ? period.planName : offer.planByKind ? `${offer.planName} · ${KIND_NAMES[kind]}` : offer.planName;
  // Pelo link de um representante: a instalação e a desinstalação são pagas direto a ele. Pelo link de
  // pagamento: só a instalação, direto ao instalador (a desinstalação, quando há, é cobrada no fim).
  const install = chosenInstall(offer, choice);
  const directInstall = offer.installDirect ? install.cents : 0;
  const directUninstall = offer.installDirect && !offer.link ? ', paga direto ao representante' : '';
  const tracker = offer.tracker;
  // Com o cupom em mais de uma cobrança: as seguintes com o desconto, antes do valor normal.
  const moreDiscounted = coupon && totals.couponCents > 0 ? totals.couponCharges - 1 : 0;
  // O cupom para sempre: o valor de depois já é o com o desconto.
  const forever = coupon && totals.couponCents > 0 && totals.couponForever ? ', com o cupom, para sempre,' : '';
  return (
    <div className={styles.summary} aria-label="Resumo do pedido">
      <p className={styles.legend}>Resumo · {vehicle}</p>
      <div className={styles.line}>
        <span>{period ? `${period.months === 12 ? '1º ano' : '1º semestre'} · ${plan}` : `1ª mensalidade · ${plan}`}</span>
        <span>{formatMoney(totals.firstMonthlyCents + totals.couponCents)}</span>
      </div>
      {coupon && totals.couponCents > 0 && (
        <div className={`${styles.line} ${styles.discount}`}>
          <span>Cupom {coupon.code}</span>
          <span>−{formatMoney(totals.couponCents)}</span>
        </div>
      )}
      {!loan && (
        <div className={styles.line}>
          <span>Rastreador (fica com você)</span>
          <span>incluso</span>
        </div>
      )}
      {totals.installNowCents > 0 && (
        <div className={styles.line}>
          <span>Instalação pelos instaladores da Farbo{install.blocking && ', com bloqueio'}</span>
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
        <span>{offer.card ? 'Hoje' : 'Hoje, no Pix'}</span>
        <span>{freightPending ? '—' : formatMoney(totals.totalNowCents)}</span>
      </div>
      {period ? (
        <p className={styles.note}>
          {moreDiscounted > 0 ? (
            <>
              Depois, mais {moreDiscounted} {moreDiscounted === 1 ? 'cobrança' : 'cobranças'} de{' '}
              <span className={styles.monthly}>{formatMoney(totals.firstMonthlyCents)}</span>, com o cupom, e, a partir da{' '}
              {totals.couponCharges + 1}ª, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}</span> a cada{' '}
              {period.months} meses, no dia {offer.dueDay}.{' '}
            </>
          ) : (
            <>
              Depois, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}</span>
              {forever} a cada {period.months} meses, no dia {offer.dueDay}.{' '}
            </>
          )}
          {loan
            ? `O rastreador é emprestado${totals.guaranteeCents > 0 ? `; ${formatMoney(totals.guaranteeCents)} do valor é a garantia de troca dele` : ''}.`
            : 'O rastreador incluso fica com você e já está no valor.'}{' '}
          Se cancelar antes do fim do período pago, não há devolução: o rastreamento segue até o fim dele e a assinatura
          não renova
          {loan &&
            offer.loan &&
            offer.loan.uninstallFeeCents > 0 &&
            `; no fim, a desinstalação custa ${formatMoney(offer.loan.uninstallFeeCents)}${directUninstall}`}
          .
        </p>
      ) : (
      <p className={styles.note}>
        {totals.trackerMonths > 1 ? (
          <>
            Depois, mais {totals.trackerMonths - 1} mensalidades de{' '}
            <span className={styles.monthly}>{formatMoney(totals.firstMonthlyCents + totals.couponCents)}</span> e, a partir da{' '}
            {totals.trackerMonths + 1}ª, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span>, no
            dia {offer.dueDay}
          </>
        ) : moreDiscounted > 0 ? (
          <>
            Depois, mais {moreDiscounted} {moreDiscounted === 1 ? 'mensalidade' : 'mensalidades'} de{' '}
            <span className={styles.monthly}>{formatMoney(totals.firstMonthlyCents)}</span>, com o cupom, e, a partir da{' '}
            {totals.couponCharges + 1}ª, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span>, no dia{' '}
            {offer.dueDay}
          </>
        ) : (
          <>
            Depois, <span className={styles.monthly}>{formatMoney(totals.monthlyCents)}/mês</span>
            {forever} no dia {offer.dueDay}
          </>
        )}
        {loan && totals.guaranteeCents > 0 && `; ${formatMoney(totals.guaranteeCents)} dela é a garantia de troca do rastreador`}
        {loan &&
          offer.loan &&
          offer.loan.uninstallFeeCents > 0 &&
          `. Ao encerrar, a desinstalação custa ${formatMoney(offer.loan.uninstallFeeCents)}${directUninstall}`}
        .
      </p>
      )}
      {!loan && !period && tracker.cents > 0 && tracker.months > 0 && (
        <p className={styles.note}>
          O rastreador incluso fica com você. Se a assinatura for encerrada antes da {tracker.months}ª mensalidade, a parte
          dele que falta ({formatMoney(tracker.cents)} por mensalidade) vence de uma vez.
        </p>
      )}
      {directInstall > 0 && (
        <p className={styles.note}>
          A instalação{install.blocking && ' com bloqueio'} ({formatMoney(directInstall)}) é paga direto ao{' '}
          {offer.link ? 'instalador' : 'representante'}, no dia, fora do Pix.
        </p>
      )}
    </div>
  );
}
