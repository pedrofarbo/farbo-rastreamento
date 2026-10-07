import type { ContractBlock, ContractDocument } from '@/types';

import styles from './Contract.module.css';

/** Um parágrafo ou uma lista do contrato. */
export function ContractBlocks({ blocks }: { blocks: ContractBlock[] }) {
  return (
    <>
      {blocks.map((b, i) =>
        b.kind === 'ul' ? (
          <ul key={i}>
            {(b.items ?? []).map((item, j) => (
              <li key={j}>{item}</li>
            ))}
          </ul>
        ) : (
          <p key={i}>{b.text}</p>
        ),
      )}
    </>
  );
}

/** O contrato inteiro: o título, a versão e as cláusulas. */
export function ContractText({ contract }: { contract: ContractDocument }) {
  return (
    <div className={styles.text}>
      <h2 className={styles.textTitle}>{contract.title}</h2>
      <ContractBlocks blocks={contract.intro} />
      {contract.sections.map((section) => (
        <section key={section.title}>
          <h3>{section.title}</h3>
          <ContractBlocks blocks={section.blocks} />
        </section>
      ))}
    </div>
  );
}
