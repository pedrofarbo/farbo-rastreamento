import React, { useEffect, useRef, useState } from 'react';

import { CineHero } from '@/components/cinema/CineHero';
import { DeviceReveal } from '@/components/cinema/DeviceReveal';
import { Features } from '@/components/cinema/Features';
import { Finale, PlansIntro, ScrollProgress } from '@/components/cinema/Finale';
import { Journey } from '@/components/cinema/Journey';
import { LockStory } from '@/components/cinema/LockStory';
import { CheckoutSection } from '@/components/landing/CheckoutSection';
import { ContactModal } from '@/components/landing/ContactModal';
import { Footer } from '@/components/landing/Footer';
import { InstallationSection } from '@/components/landing/InstallationSection';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { LeadModal } from '@/components/landing/LeadModal';
import { Navbar } from '@/components/landing/Navbar';
import { PricingSection } from '@/components/landing/PricingSection';
import { RepresentativesSection } from '@/components/landing/RepresentativesSection';
import { TestimonialsSection } from '@/components/landing/TestimonialsSection';
import { WhatsAppFloat } from '@/components/landing/WhatsAppFloat';
import { HIRE_VIA_WHATSAPP, WHATSAPP_NUMBER } from '@/config/contact';
import { PRE_LAUNCH } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import { useCineReveal, useScenes } from '@/hooks/useScenes';
import { clickEvent, track, trackPageview } from '@/services/analytics';
import { referralCode, saveReferral } from '@/services/referral';

import styles from './LandingPage.module.css';

/**
 * A landing cinematográfica. A história é contada em cenas que andam com a
 * rolagem (components/cinema, com o useScenes): a abertura com o mapa e o
 * preço, o bloqueio acontecendo, o que vem no plano, o rastreador, o passo a
 * passo e o fim. O que tem regra de negócio continua com os componentes de
 * sempre (components/landing): os planos e a frota, a instalação e os
 * prestadores, os representantes, a contratação pelo site, o pré-cadastro, o
 * menu e o rodapé. Os preços vêm todos da oferta do servidor.
 */
export const LandingPage: React.FC = () => {
  const [modalOpen, setModalOpen] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState<string>('Plano Mensal');
  // Prestadores recomendados: nulo fechado; senão, o filtro inicial.
  const [installers, setInstallers] = useState<'todos' | 'moto' | 'carro' | null>(null);
  const { open: checkoutOpen } = useCheckoutOffer();

  const handleOpenModal = (plan?: string) => {
    if (plan) setSelectedPlan(plan);
    setModalOpen(true);
    track('lead_open', plan ?? '');
  };

  const openInstallers = (filter: 'todos' | 'moto' | 'carro' = 'todos') => {
    setInstallers(filter);
    track('installers_open', filter);
  };

  const wrapper = useRef<HTMLDivElement>(null);
  // Primeiro as cenas: as presas só ganham altura com o [data-motion], e a
  // rolagem até a âncora (logo abaixo) precisa delas no tamanho certo.
  useScenes(wrapper);
  useCineReveal(wrapper);

  // A visita, e cada seção a que a pessoa chega (uma vez por visita): mostra
  // até onde a página é lida.
  useEffect(() => {
    // Vindo de outra página com uma seção no endereço (/#planos): a página
    // monta depois do navegador procurar a âncora, então rola aqui.
    if (window.location.hash.length > 1) {
      document.getElementById(decodeURIComponent(window.location.hash.slice(1)))?.scrollIntoView();
    }
    // ?ref=<código>: o link de um afiliado que aponta direto para a landing.
    const ref = new URLSearchParams(window.location.search).get('ref');
    if (ref) saveReferral(ref);
    trackPageview(ref ? { source: 'indicacao', campaign: referralCode(ref) } : {});
    const root = wrapper.current;
    if (!root || typeof IntersectionObserver === 'undefined') return;
    const seen = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const id = (entry.target as HTMLElement).dataset.section ?? entry.target.id;
          if (!entry.isIntersecting || !id || seen.has(id)) continue;
          seen.add(id);
          track('section_view', id);
          observer.unobserve(entry.target);
        }
      },
      // As cenas presas têm várias telas de altura: nunca ficam 30% visíveis
      // (o limiar da clássica). Conta quando a seção chega aos 70% de cima da tela.
      { threshold: 0, rootMargin: '0px 0px -30% 0px' },
    );
    root.querySelectorAll('main section[id], footer').forEach((el) => {
      if (el.tagName === 'FOOTER') (el as HTMLElement).dataset.section = 'rodape';
      observer.observe(el);
    });
    return () => observer.disconnect();
  }, []);

  // Os botões marcados com data-analytics (um ouvinte só, na página toda).
  const onClickCapture = (event: React.MouseEvent) => {
    const hit = clickEvent(event.target as Element);
    if (hit) track(hit.name, hit.label);
  };

  return (
    <div className={styles.root} ref={wrapper} onClickCapture={onClickCapture}>
      <ScrollProgress />
      <Navbar onOpenModal={handleOpenModal} />
      <main>
        <CineHero onOpenModal={handleOpenModal} />
        <LockStory />
        <Features />
        <DeviceReveal />
        <Journey />
        <PlansIntro />
        <PricingSection onOpenModal={handleOpenModal} />
        <InstallationSection onOpenInstallers={openInstallers} />
        {/* Os representantes oficiais que a central marcou (nenhum: sai). */}
        <RepresentativesSection />
        {/* A chamada para a contratação pelo site (fechada na central, sai) e,
            quando houver clientes para depor, os depoimentos. */}
        {checkoutOpen && <CheckoutSection onOpenModal={handleOpenModal} />}
        {!PRE_LAUNCH && <TestimonialsSection />}
        <Finale onOpenModal={handleOpenModal} />
      </main>
      <Footer />

      {/* A contratação abre o pré-cadastro (pré-cliente), com o WhatsApp como
          canal ao lado; HIRE_VIA_WHATSAPP volta ao modal que abre a conversa. */}
      {HIRE_VIA_WHATSAPP && WHATSAPP_NUMBER ? (
        <ContactModal isOpen={modalOpen} onClose={() => setModalOpen(false)} defaultPlan={selectedPlan} />
      ) : (
        <LeadModal isOpen={modalOpen} onClose={() => setModalOpen(false)} defaultPlan={selectedPlan} />
      )}

      {/* No celular, some enquanto uma cena está presa (cobria o pé dela). */}
      {WHATSAPP_NUMBER && (
        <div className={styles.float}>
          <WhatsAppFloat />
        </div>
      )}

      <InstallersModal
        isOpen={installers !== null}
        initialFilter={installers ?? 'todos'}
        onClose={() => setInstallers(null)}
      />
    </div>
  );
};

export default LandingPage;
