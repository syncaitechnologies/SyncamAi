import { useMemo, useState } from "react";

import { Icon } from "./icon";
import {
  modelRegistryCatalog,
  filterRegistryCapabilities,
  registryCapabilities,
} from "./model-registry-model";

export function ModelRegistryWorkspace({
  dataMode,
}: {
  dataMode: "demo" | "live";
}) {
  const [query, setQuery] = useState("");
  const capabilities = useMemo(
    () => filterRegistryCapabilities(registryCapabilities, query),
    [query],
  );
  const catalogLabel =
    dataMode === "live"
      ? `Live contract unavailable · Synthetic catalog v${modelRegistryCatalog.schemaVersion}`
      : `Synthetic catalog v${modelRegistryCatalog.schemaVersion}`;

  return (
    <section className="model-registry-page" aria-labelledby="model-registry-title">
      <header className="model-registry-heading">
        <div>
          <div className="heading-kicker">
            <span className="live-pill demo-pill"><span />PLANNING</span>
            <span>AI platform · read-only</span>
          </div>
          <h1 id="model-registry-title">Model registry</h1>
          <p>Canonical capabilities and their release prerequisites.</p>
        </div>
        <div className="model-registry-count">
          <strong>{registryCapabilities.length}</strong>
          <span>planned capabilities</span>
        </div>
      </header>

      <div className="model-registry-boundary" role="status">
        <Icon name="shield" size={17} />
        <span>
          <strong>External model promotion is blocked</strong>
          <small>
            ADR-001 is awaiting Legal approval. This workspace has no model
            artifacts, evidence, evaluation results, activation, or release controls.
          </small>
        </span>
      </div>

      <div className="model-registry-toolbar">
        <label htmlFor="model-registry-search">Search planned capabilities</label>
        <div>
          <Icon name="search" size={15} />
          <input
            id="model-registry-search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Name, family, or owner"
          />
        </div>
        <span>{catalogLabel}</span>
      </div>

      {capabilities.length ? (
        <div className="model-registry-grid" aria-label="Planned AI capabilities">
          {capabilities.map((capability) => (
            <article className="model-registry-card" key={capability.id}>
              <div className="model-registry-card-top">
                <span className="model-status">Blocked</span>
                <code>{capability.id}</code>
              </div>
              <h2>{capability.name}</h2>
              <dl>
                <div><dt>Family</dt><dd>{capability.family}</dd></div>
                <div><dt>Owner</dt><dd>{capability.owner}</dd></div>
              </dl>
              <footer>
                <span>Evaluation planned</span>
                <span>Legal gate required</span>
              </footer>
            </article>
          ))}
        </div>
      ) : (
        <div className="model-registry-empty">
          <Icon name="search" size={20} />
          <h2>No planned capability found</h2>
          <p>Change the search term; this catalog does not create synthetic matches.</p>
        </div>
      )}
    </section>
  );
}
