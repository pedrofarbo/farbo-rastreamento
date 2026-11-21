import { useEffect, type RefObject } from 'react';

const clamp01 = (n: number) => (n < 0 ? 0 : n > 1 ? 1 : n);

/** Um SVG com animação SMIL (animateMotion) cujo tempo segue a rolagem. */
interface Smil {
  svg: SVGSVGElement;
  dur: number;
  from: number;
  to: number;
  t: number;
}

interface Scene {
  el: HTMLElement;
  smils: Smil[];
  pinned: boolean;
  snap: boolean;
  steps: number;
  step: number;
  live: boolean;
  p: number;
  top: number;
  height: number;
}

/** "Reduzir movimento" ligado (ou sem matchMedia, como nos testes): nada anima. */
export function prefersStill(): boolean {
  return typeof matchMedia !== 'function' || matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/**
 * O "diretor de cena" da landing: um só listener de rolagem (em
 * requestAnimationFrame) para a página toda. Cada [data-scene] recebe --p, de
 * 0 a 1, enquanto atravessa a tela ("pin": enquanto o palco fica preso;
 * "view": de quando entra até sair). Os efeitos são CSS em cima de --p.
 * Também:
 *
 * - data-steps="n": data-step com o passo atual (0 a n-1); com data-snap,
 *   --ps anda "passo a passo" (para um pouco em cada um);
 * - [data-tilt]: --rx/--ry (graus) e --mx/--my (ponteiro, em %), só com mouse;
 * - data-live na cena que está na tela (ou perto): só aí o CSS põe will-change;
 * - svg[data-smil="segundos"]: as animações SMIL do SVG (o veículo andando
 *   pela rota, com animateMotion) ficam pausadas e o tempo delas segue a
 *   rolagem: de 0 a "segundos" enquanto --p (ou --ps) vai de data-from a
 *   data-to (0 e 1 por padrão);
 * - na raiz: --progress (0 a 1, a barra de progresso) e data-pinned enquanto
 *   um palco está preso na tela (o CSS esconde o que flutua por cima dele).
 *
 * As cenas são medidas só quando o layout muda (início, resize,
 * ResizeObserver); a cada quadro, só o scrollY é lido. Com "reduzir
 * movimento", nada liga e a raiz não ganha [data-motion]: as cenas ficam no
 * estado final, como um bloco comum.
 */
export function useScenes(rootRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const root = rootRef.current;
    if (!root || prefersStill() || typeof ResizeObserver === 'undefined') return;

    const scenes: Scene[] = Array.from(root.querySelectorAll<HTMLElement>('[data-scene]')).map((el) => ({
      el,
      smils: Array.from(el.querySelectorAll<SVGSVGElement>('svg[data-smil]'))
        .filter((svg) => typeof svg.pauseAnimations === 'function')
        .map((svg) => {
          svg.pauseAnimations();
          return {
            svg,
            dur: Number(svg.dataset.smil),
            from: Number(svg.dataset.from ?? 0),
            to: Number(svg.dataset.to ?? 1),
            t: -1,
          };
        }),
      pinned: el.dataset.scene === 'pin',
      snap: el.hasAttribute('data-snap'),
      steps: Number(el.dataset.steps ?? 0),
      step: -1,
      live: false,
      p: -1,
      top: 0,
      height: 0,
    }));

    let lastProgress = -1;
    let pinnedNow: boolean | null = null;
    let frame = 0;
    let vh = window.innerHeight;
    let maxScroll = 0;

    const measure = () => {
      vh = window.innerHeight;
      const y = window.scrollY;
      for (const s of scenes) {
        const r = s.el.getBoundingClientRect();
        s.top = r.top + y;
        s.height = r.height;
      }
      maxScroll = document.documentElement.scrollHeight - vh;
    };

    const update = () => {
      frame = 0;
      const y = window.scrollY;

      let pinned = false;
      for (const s of scenes) {
        const top = s.top - y;
        if (s.pinned && top <= 1 && top + s.height >= vh - 1) pinned = true;
        const live = top < vh * 1.5 && top + s.height > -vh * 0.5;
        if (live !== s.live) {
          s.live = live;
          s.el.toggleAttribute('data-live', live);
        }
        const p = s.pinned ? clamp01(-top / Math.max(1, s.height - vh)) : clamp01((vh - top) / (vh + s.height));
        if (Math.abs(p - s.p) < 0.0004) continue;
        s.p = p;
        s.el.style.setProperty('--p', p.toFixed(4));

        let snapped = -1;
        if (s.snap && s.steps > 1) {
          // Cada trecho entre dois passos: parado nos 30% das pontas, anda (suave) no meio.
          const x = p * (s.steps - 1);
          const i = Math.min(s.steps - 2, Math.floor(x));
          const e = clamp01((x - i - 0.3) / 0.4);
          snapped = (i + e * e * (3 - 2 * e)) / (s.steps - 1);
          s.el.style.setProperty('--ps', snapped.toFixed(4));
        }
        for (const m of s.smils) {
          const base = snapped >= 0 ? snapped : p;
          const t = clamp01((base - m.from) / Math.max(0.0001, m.to - m.from)) * m.dur;
          if (Math.abs(t - m.t) > 0.002) {
            m.t = t;
            m.svg.setCurrentTime(t);
          }
        }
        if (s.steps) {
          const step =
            snapped >= 0 ? Math.round(snapped * (s.steps - 1)) : Math.min(s.steps - 1, Math.floor(p * s.steps));
          if (step !== s.step) {
            s.step = step;
            s.el.dataset.step = String(step);
          }
        }
      }

      if (pinned !== pinnedNow) {
        pinnedNow = pinned;
        root.toggleAttribute('data-pinned', pinned);
      }

      const progress = maxScroll > 0 ? clamp01(y / maxScroll) : 0;
      if (Math.abs(progress - lastProgress) > 0.0005) {
        lastProgress = progress;
        root.style.setProperty('--progress', progress.toFixed(4));
      }
    };

    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };

    // Ponteiro: só com mouse (no toque, o "tilt" atrapalha a rolagem).
    const fine = matchMedia('(hover: hover) and (pointer: fine)').matches;
    const tilts = fine ? Array.from(root.querySelectorAll<HTMLElement>('[data-tilt]')) : [];
    const onMove = (e: PointerEvent) => {
      const el = e.currentTarget as HTMLElement;
      const r = el.getBoundingClientRect();
      const x = clamp01((e.clientX - r.left) / r.width);
      const y = clamp01((e.clientY - r.top) / r.height);
      el.style.setProperty('--mx', `${(x * 100).toFixed(1)}%`);
      el.style.setProperty('--my', `${(y * 100).toFixed(1)}%`);
      el.style.setProperty('--ry', `${((x - 0.5) * 10).toFixed(2)}deg`);
      el.style.setProperty('--rx', `${((0.5 - y) * 8).toFixed(2)}deg`);
    };
    const onLeave = (e: PointerEvent) => {
      const el = e.currentTarget as HTMLElement;
      for (const prop of ['--mx', '--my', '--rx', '--ry']) el.style.removeProperty(prop);
    };
    for (const el of tilts) {
      el.addEventListener('pointermove', onMove);
      el.addEventListener('pointerleave', onLeave);
    }

    const remeasure = () => {
      measure();
      schedule();
    };
    const ro = new ResizeObserver(remeasure);

    // As cenas presas só ganham altura com [data-motion]: mede depois de ligar.
    root.setAttribute('data-motion', '');
    remeasure();
    ro.observe(root);
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', remeasure);

    return () => {
      cancelAnimationFrame(frame);
      ro.disconnect();
      window.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', remeasure);
      for (const el of tilts) {
        el.removeEventListener('pointermove', onMove);
        el.removeEventListener('pointerleave', onLeave);
      }
      for (const s of scenes) for (const m of s.smils) m.svg.unpauseAnimations();
      root.removeAttribute('data-motion');
      root.removeAttribute('data-pinned');
    };
  }, [rootRef]);

  // Parado ("reduzir movimento"): cada SVG animado congela no quadro que
  // conta a história: o fim da rolagem (data-smil) ou o de data-still.
  useEffect(() => {
    const root = rootRef.current;
    if (!root || !prefersStill()) return;
    for (const svg of root.querySelectorAll<SVGSVGElement>('svg[data-smil], svg[data-still]')) {
      if (typeof svg.pauseAnimations !== 'function') continue;
      svg.pauseAnimations();
      svg.setCurrentTime(Number(svg.dataset.still ?? svg.dataset.smil ?? 0));
    }
  }, [rootRef]);
}

/**
 * Entradas ao rolar ([data-cine-reveal]): o que já está na tela é marcado
 * antes de ligar (nada pisca); o resto aparece ao entrar na viewport. Sem
 * IntersectionObserver ou com "reduzir movimento", tudo já aparece.
 */
export function useCineReveal(rootRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const root = rootRef.current;
    if (!root || typeof IntersectionObserver === 'undefined' || prefersStill()) return;
    const nodes = Array.from(root.querySelectorAll<HTMLElement>('[data-cine-reveal]:not([data-shown])'));
    const fold = window.innerHeight;
    const pending = nodes.filter((el) => {
      if (el.getBoundingClientRect().top < fold) {
        el.setAttribute('data-shown', '');
        return false;
      }
      return true;
    });
    const io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          entry.target.setAttribute('data-shown', '');
          io.unobserve(entry.target);
        }
      },
      { rootMargin: '0px 0px -8% 0px', threshold: 0.08 },
    );
    pending.forEach((el) => io.observe(el));
    root.setAttribute('data-reveal-ready', '');
    return () => {
      io.disconnect();
      root.removeAttribute('data-reveal-ready');
    };
  }, [rootRef]);
}
