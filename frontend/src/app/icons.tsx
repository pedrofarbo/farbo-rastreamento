/** Ícones do app (traço simples, herdam a cor do texto). */
const base = { width: 24, height: 24, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2,
  strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true };

export const MapIcon = () => (
  <svg {...base}>
    <path d="M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21z" />
    <circle cx="12" cy="9.5" r="2.5" />
  </svg>
);

export const CarIcon = () => (
  <svg {...base}>
    <path d="M5 16h14l-1.6-5.2A2 2 0 0 0 15.5 9.4h-7a2 2 0 0 0-1.9 1.4L5 16z" />
    <path d="M4 16h16v3H4z" />
    <circle cx="7.5" cy="19" r="1" />
    <circle cx="16.5" cy="19" r="1" />
  </svg>
);

export const BellIcon = () => (
  <svg {...base}>
    <path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15L6 16z" />
    <path d="M10 20a2 2 0 0 0 4 0" />
  </svg>
);

export const ReceiptIcon = () => (
  <svg {...base}>
    <path d="M6 3h12v18l-3-2-3 2-3-2-3 2V3z" />
    <path d="M9 8h6M9 12h6" />
  </svg>
);

export const UserIcon = () => (
  <svg {...base}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21a8 8 0 0 1 16 0" />
  </svg>
);

export const BackIcon = () => (
  <svg {...base}>
    <path d="M15 18l-6-6 6-6" />
  </svg>
);

export const ChevronIcon = () => (
  <svg {...base} width={18} height={18}>
    <path d="M9 6l6 6-6 6" />
  </svg>
);

export const ExpandIcon = () => (
  <svg {...base} width={20} height={20}>
    <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
  </svg>
);

export const CloseIcon = () => (
  <svg {...base} width={22} height={22}>
    <path d="M6 6l12 12M18 6L6 18" />
  </svg>
);

/** Mira de "centralizar no veículo"; cheia enquanto o mapa segue o veículo. */
export const LocateIcon = ({ active = false }: { active?: boolean }) => (
  <svg {...base} width={22} height={22}>
    <circle cx="12" cy="12" r="7" />
    <circle cx="12" cy="12" r="2.5" fill={active ? 'currentColor' : 'none'} />
    <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
  </svg>
);

/** Face ID (iPhone). */
export const FaceIdIcon = ({ size = 24 }: { size?: number }) => (
  <svg {...base} width={size} height={size}>
    <path d="M4 8V6a2 2 0 0 1 2-2h2M16 4h2a2 2 0 0 1 2 2v2M20 16v2a2 2 0 0 1-2 2h-2M8 20H6a2 2 0 0 1-2-2v-2" />
    <path d="M9 9v1.5M15 9v1.5M12 9v4.5h-1M9.5 16a3.5 3.5 0 0 0 5 0" />
  </svg>
);

/** Digital (Android e os demais). */
export const FingerprintIcon = ({ size = 24 }: { size?: number }) => (
  <svg {...base} width={size} height={size}>
    <path d="M6.5 7.5A7 7 0 0 1 19 12v1M5 11.5V12a7 7 0 0 0 1 3.6M12 8a4 4 0 0 1 4 4v1.5a9 9 0 0 1-1 4M8 12a4 4 0 0 1 .6-2M12 12v2a12 12 0 0 1-2 6.5M8.5 15.5A10 10 0 0 1 8 18" />
  </svg>
);
