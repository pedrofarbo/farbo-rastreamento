/**
 * Os esquemas de ligação do J16 da página de instalação: o chicote do
 * rastreador e o mini relê de bloqueio. As cores e os pinos vêm do manual do
 * fabricante (família GT06/Concox) e conferem com o kit que a Farbo envia:
 * chicote vermelho, preto, laranja e amarelo; relê com branco, amarelo e dois
 * verdes. Fundo claro, para os fios escuros aparecerem e para imprimir.
 */

const WIRE = { red: '#dc2626', black: '#111827', orange: '#ea580c', yellow: '#ca8a04', white: '#f8fafc', green: '#16a34a' };

function Label({ x, y, title, sub }: { x: number; y: number; title: string; sub: string }) {
  return (
    <>
      <text x={x} y={y - 3} fontSize="13" fontWeight="700" fill="#0d130e">
        {title}
      </text>
      <text x={x} y={y + 12} fontSize="10.5" fill="#475569">
        {sub}
      </text>
    </>
  );
}

/** O chicote: os quatro fios do rastreador e onde cada um vai. */
export function HarnessDiagram() {
  const wires = [
    { y: 50, color: WIRE.red, name: 'vermelho', title: '+12V permanente', sub: 'energia mesmo desligado' },
    { y: 86, color: WIRE.black, name: 'preto', title: 'Terra', sub: 'negativo ou chassi' },
    { y: 122, color: WIRE.orange, name: 'laranja', title: 'Pós-chave', sub: 'energia só com a ignição' },
    { y: 158, color: WIRE.yellow, name: 'amarelo', title: 'Relê de bloqueio', sub: 'no amarelo do relê' },
  ];
  return (
    <svg viewBox="0 0 340 196" role="img" aria-labelledby="chicote-titulo chicote-desc" width="100%">
      <title id="chicote-titulo">Ligação do chicote do rastreador J16</title>
      <desc id="chicote-desc">
        Fio vermelho no +12V permanente, preto no terra, laranja no pós-chave e amarelo no fio amarelo do relê de
        bloqueio.
      </desc>
      <rect x="12" y="28" width="92" height="152" rx="12" fill="#0d130e" />
      <text x="58" y="98" textAnchor="middle" fontSize="20" fontWeight="800" fill="#3be558">
        J16
      </text>
      <text x="58" y="116" textAnchor="middle" fontSize="10" fill="#cbd5e1">
        rastreador
      </text>
      {wires.map((w) => (
        <g key={w.name}>
          <line x1="104" y1={w.y} x2="186" y2={w.y} stroke={w.color} strokeWidth="5" strokeLinecap="round" />
          {w.color === WIRE.black ? null : (
            <line x1="104" y1={w.y} x2="186" y2={w.y} stroke="#0d130e" strokeOpacity="0.15" strokeWidth="1" />
          )}
          <text x="145" y={w.y - 7} textAnchor="middle" fontSize="10" fill="#475569">
            {w.name}
          </text>
          <circle cx="190" cy={w.y} r="4" fill="#0d130e" />
          <Label x={200} y={w.y} title={w.title} sub={w.sub} />
        </g>
      ))}
    </svg>
  );
}

/** O mini relê: o branco no +12V, o amarelo no rastreador e os verdes em série no fio da bomba. */
export function RelayDiagram() {
  return (
    <svg viewBox="0 0 340 256" role="img" aria-labelledby="rele-titulo rele-desc" width="100%">
      <title id="rele-titulo">Ligação do mini relê de bloqueio</title>
      <desc id="rele-desc">
        Fio branco do relê no +12V permanente, fio amarelo do relê no fio amarelo do rastreador, e os dois fios verdes
        nas duas pontas do fio de alimentação da bomba de combustível, que é cortado.
      </desc>
      <rect x="12" y="20" width="92" height="130" rx="12" fill="#0d130e" />
      <text x="58" y="80" textAnchor="middle" fontSize="15" fontWeight="800" fill="#3be558">
        Relê
      </text>
      <text x="58" y="97" textAnchor="middle" fontSize="10" fill="#cbd5e1">
        12V
      </text>

      {/* O branco e o amarelo acionam o relê. */}
      <line x1="104" y1="44" x2="186" y2="44" stroke="#cbd5e1" strokeWidth="7" strokeLinecap="round" />
      <line x1="104" y1="44" x2="186" y2="44" stroke={WIRE.white} strokeWidth="5" strokeLinecap="round" />
      <text x="145" y="36" textAnchor="middle" fontSize="10" fill="#475569">
        branco (85)
      </text>
      <circle cx="190" cy="44" r="4" fill="#0d130e" />
      <Label x={200} y={44} title="+12V permanente" sub="o mesmo do fio vermelho" />

      <line x1="104" y1="80" x2="186" y2="80" stroke={WIRE.yellow} strokeWidth="5" strokeLinecap="round" />
      <text x="145" y="72" textAnchor="middle" fontSize="10" fill="#475569">
        amarelo (86)
      </text>
      <circle cx="190" cy="80" r="4" fill="#0d130e" />
      <Label x={200} y={80} title="Rastreador" sub="no fio amarelo do chicote" />

      {/* Os verdes, em série no fio cortado da bomba: cada um numa ponta. */}
      <line x1="12" y1="214" x2="140" y2="214" stroke="#64748b" strokeWidth="4" strokeLinecap="round" />
      <line x1="200" y1="214" x2="328" y2="214" stroke="#64748b" strokeWidth="4" strokeLinecap="round" />
      <path d="M 40 150 L 40 192 L 140 192 L 140 214" fill="none" stroke={WIRE.green} strokeWidth="5" strokeLinejoin="round" />
      <path d="M 76 150 L 76 172 L 200 172 L 200 214" fill="none" stroke={WIRE.green} strokeWidth="5" strokeLinejoin="round" />
      <text x="96" y="186" textAnchor="middle" fontSize="10" fill="#475569">
        verde
      </text>
      <text x="150" y="166" textAnchor="middle" fontSize="10" fill="#475569">
        verde
      </text>
      <circle cx="140" cy="214" r="5" fill="#0d130e" />
      <circle cx="200" cy="214" r="5" fill="#0d130e" />
      <text x="170" y="219" textAnchor="middle" fontSize="16" fill="#dc2626">
        ✂
      </text>
      <text x="170" y="236" textAnchor="middle" fontSize="11.5" fontWeight="700" fill="#0d130e">
        Fio de alimentação da bomba de combustível, cortado
      </text>
      <text x="170" y="248" textAnchor="middle" fontSize="10" fill="#475569">
        um verde em cada ponta (tanto faz qual)
      </text>
    </svg>
  );
}
