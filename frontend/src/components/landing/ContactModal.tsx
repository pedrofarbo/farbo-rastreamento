import React, { useState } from 'react';
import { WHATSAPP_NUMBER } from '@/config/contact';

import styles from './ContactModal.module.css';
import { useModalBehavior } from './useModalBehavior';

interface ContactModalProps {
  isOpen: boolean;
  onClose: () => void;
  defaultPlan?: string;
}

export const ContactModal: React.FC<ContactModalProps> = ({
  isOpen,
  onClose,
  defaultPlan = 'Plano Mensal',
}) => {
  const [vehicleType, setVehicleType] = useState<'moto' | 'carro' | 'frota'>('moto');
  const [vehicleCount, setVehicleCount] = useState<number>(1);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [selectedPlan, setSelectedPlan] = useState(defaultPlan);
  useModalBehavior(isOpen, onClose);

  if (!isOpen) return null;

  const handleWhatsAppSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const message = encodeURIComponent(
      `Olá! Gostaria de cotar o rastreamento Farbo.\n` +
      `• Nome: ${name || 'Não informado'}\n` +
      `• Plano de interesse: ${selectedPlan}\n` +
      `• Tipo de veículo: ${vehicleType.toUpperCase()}\n` +
      `• Qtd de veículos: ${vehicleCount}\n` +
      `• Telefone: ${phone || 'Não informado'}`
    );
    window.open(`https://wa.me/${WHATSAPP_NUMBER}?text=${message}`, '_blank', 'noopener,noreferrer');
    onClose();
  };

  return (
    <div className={styles.overlay} onClick={onClose} id="contact-modal-overlay">
      <div className={styles.modal} onClick={(e) => e.stopPropagation()}>
        <button type="button" className={styles.closeBtn} onClick={onClose} aria-label="Fechar modal">
          ✕
        </button>

        <div className={styles.header}>
          <div className={styles.badge}>Atendimento Rápido</div>
          <h2>Quero meu rastreador</h2>
          <p>Preencha os dados abaixo para receber atendimento prioritário no WhatsApp.</p>
        </div>

        <form onSubmit={handleWhatsAppSubmit} className={styles.form}>
          <div className={styles.formGroup}>
            <label htmlFor="modal-name">Seu Nome</label>
            <input
              id="modal-name"
              type="text"
              autoComplete="name"
              autoCapitalize="words"
              enterKeyHint="next"
              placeholder="Digite seu nome completo"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          <div className={styles.formGroup}>
            <label htmlFor="modal-phone">WhatsApp / Telefone</label>
            <input
              id="modal-phone"
              type="tel"
              inputMode="tel"
              autoComplete="tel"
              enterKeyHint="next"
              placeholder="(11) 99999-9999"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              required
            />
          </div>

          <div className={styles.formRow}>
            <div className={styles.formGroup}>
              <label htmlFor="modal-plan">Plano Escolhido</label>
              <select
                id="modal-plan"
                value={selectedPlan}
                onChange={(e) => setSelectedPlan(e.target.value)}
              >
                <option value="Plano Mensal - R$ 69,90">Plano Mensal (R$ 69,90/mês)</option>
                <option value="Preço Especial Insanos MC - R$ 39,90">Insanos MC (R$ 39,90/mês)</option>
                <option value="Apenas Equipamento - R$ 150,00">Apenas Equipamento (R$ 150,00)</option>
              </select>
            </div>

            <div className={styles.formGroup}>
              <label htmlFor="modal-vehicle-type">Tipo de Veículo</label>
              <div className={styles.typeSelector}>
                <button
                  type="button"
                  className={vehicleType === 'moto' ? styles.activeType : ''}
                  onClick={() => setVehicleType('moto')}
                >
                  🏍 Moto
                </button>
                <button
                  type="button"
                  className={vehicleType === 'carro' ? styles.activeType : ''}
                  onClick={() => setVehicleType('carro')}
                >
                  🚗 Carro
                </button>
                <button
                  type="button"
                  className={vehicleType === 'frota' ? styles.activeType : ''}
                  onClick={() => setVehicleType('frota')}
                >
                  🚚 Frota
                </button>
              </div>
            </div>
          </div>

          <div className={styles.formGroup}>
            <label htmlFor="modal-count">Quantidade de veículos: {vehicleCount}</label>
            <input
              id="modal-count"
              type="range"
              min="1"
              max="20"
              value={vehicleCount}
              onChange={(e) => setVehicleCount(Number(e.target.value))}
              className={styles.rangeInput}
            />
          </div>

          <button type="submit" className={styles.submitBtn}>
            <span>Iniciar Atendimento via WhatsApp</span>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14M12 5l7 7-7 7" />
            </svg>
          </button>
        </form>
      </div>
    </div>
  );
};
