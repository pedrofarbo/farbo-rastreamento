import React, { useState } from 'react';
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
import styles from './LandingPage.module.css';

export const LandingPage: React.FC = () => {
  const [modalOpen, setModalOpen] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState<string>('Plano Mensal');
  // Prestadores recomendados: nulo fechado; senão, o filtro inicial.
  const [installers, setInstallers] = useState<'todos' | 'moto' | 'carro' | null>(null);

  const handleOpenModal = (plan?: string) => {
    if (plan) setSelectedPlan(plan);
    setModalOpen(true);
  };

  const handleCloseModal = () => {
    setModalOpen(false);
  };

  return (
    <div className={styles.landingWrapper}>
      <Navbar onOpenModal={handleOpenModal} />
      <main>
        <HeroSection onOpenModal={handleOpenModal} />
        <PricingSection onOpenModal={handleOpenModal} />
        <InstallationSection onOpenInstallers={(filter = 'todos') => setInstallers(filter)} />
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
