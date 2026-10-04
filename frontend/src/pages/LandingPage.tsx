import React, { useEffect, useRef, useState } from 'react';
import { Navbar } from '@/components/landing/Navbar';
import { HeroSection } from '@/components/landing/HeroSection';
import { PricingSection } from '@/components/landing/PricingSection';
import { InstallationSection } from '@/components/landing/InstallationSection';
import { FeaturesSection } from '@/components/landing/FeaturesSection';
import { HowItWorksSection } from '@/components/landing/HowItWorksSection';
import { TestimonialsSection } from '@/components/landing/TestimonialsSection';
import { CTASection } from '@/components/landing/CTASection';
import { Footer } from '@/components/landing/Footer';
import { ContactModal } from '@/components/landing/ContactModal';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { LaunchSection } from '@/components/landing/LaunchSection';
import { LeadModal } from '@/components/landing/LeadModal';
import { WHATSAPP_NUMBER } from '@/config/contact';
import { PRE_LAUNCH } from '@/config/landing';
import { clickEvent, track, trackPageview } from '@/services/analytics';
import styles from './LandingPage.module.css';

export const LandingPage: React.FC = () => {
  const [modalOpen, setModalOpen] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState<string>('Plano Mensal');
  // Prestadores recomendados: nulo fechado; senão, o filtro inicial.
  const [installers, setInstallers] = useState<'todos' | 'moto' | 'carro' | null>(null);

  const handleOpenModal = (plan?: string) => {
    if (plan) setSelectedPlan(plan);
    setModalOpen(true);
    track('lead_open', plan ?? '');
  };

  const openInstallers = (filter: 'todos' | 'moto' | 'carro' = 'todos') => {
    setInstallers(filter);
    track('installers_open', filter);
  };

  // A visita, e cada seção a que a pessoa chega (uma vez por visita): mostra
  // até onde a página é lida.
  const wrapper = useRef<HTMLDivElement>(null);
  useEffect(() => {
    trackPageview();
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
      { threshold: 0.3 },
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

  const handleCloseModal = () => {
    setModalOpen(false);
  };

  return (
    <div className={styles.landingWrapper} ref={wrapper} onClickCapture={onClickCapture}>
      <Navbar onOpenModal={handleOpenModal} />
      <main>
        <HeroSection onOpenModal={handleOpenModal} />
        <PricingSection onOpenModal={handleOpenModal} />
        <InstallationSection onOpenInstallers={openInstallers} />
        <FeaturesSection />
        <HowItWorksSection />
        {/* Sem clientes ativos ainda: no lugar dos depoimentos, a lista de quem
            quer ser avisado do lançamento. */}
        {PRE_LAUNCH ? <LaunchSection /> : <TestimonialsSection />}
        <CTASection onOpenModal={handleOpenModal} />
      </main>
      <Footer />

      {/* Com o WhatsApp oficial, a contratação abre a conversa; sem ele, o
          cadastro de interesse (pré-cliente). */}
      {WHATSAPP_NUMBER ? (
        <ContactModal isOpen={modalOpen} onClose={handleCloseModal} defaultPlan={selectedPlan} />
      ) : (
        <LeadModal isOpen={modalOpen} onClose={handleCloseModal} defaultPlan={selectedPlan} />
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
