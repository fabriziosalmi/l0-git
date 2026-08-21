<script setup>
defineProps({
  id:       { type: String, required: true },
  severity: { type: String, required: true },
  tags:     { type: String, default: '' },
  scope:    { type: String, default: '' }
})
</script>

<template>
  <dl class="gate-meta">
    <div>
      <dt>Gate ID</dt>
      <dd><code>{{ id }}</code></dd>
    </div>
    <div>
      <dt>Severity</dt>
      <dd><span class="sev" :class="'sev--' + severity">{{ severity }}</span></dd>
    </div>
    <div v-if="scope">
      <dt>Scope</dt>
      <dd v-html="scope.replace(/`([^`]+)`/g, '<code>$1</code>')" />
    </div>
    <div v-if="tags">
      <dt>Tags</dt>
      <dd class="tags">
        <span v-for="t in tags.split(',')" :key="t" class="tag">{{ t.trim() }}</span>
      </dd>
    </div>
  </dl>
</template>

<style scoped>
@media (max-width: 520px) {
  .gate-meta { grid-template-columns: minmax(0, 1fr); }
}
.gate-meta {
  /* Fixed two-column grid, not auto-fit: with exactly four cells, auto-fit
     leaves a hole whenever the column count is odd, and the 1px gap paints
     that hole as a grey block. */
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1px;
  margin: 24px 0 32px;
  padding: 1px;
  background: var(--vp-c-divider);
  border: 1px solid var(--vp-c-divider);
  border-radius: 10px;
  overflow: hidden;
}
.gate-meta > div {
  background: var(--vp-c-bg);
  padding: 12px 16px;
}
.gate-meta dt {
  font-size: 10.5px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--vp-c-text-3);
  margin-bottom: 6px;
}
.gate-meta dd {
  margin: 0;
  font-size: 13.5px;
  line-height: 1.45;
  color: var(--vp-c-text-1);
}
.gate-meta dd :deep(code),
.gate-meta dd code {
  font-size: 12.5px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--vp-c-bg-soft);
}
.sev {
  display: inline-block;
  padding: 1px 9px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid currentColor;
}
.sev--error   { color: #C0392B; }
.sev--warning { color: #9A6700; }
.sev--info    { color: #57606A; }
.dark .sev--error   { color: #FF7B72; }
.dark .sev--warning { color: #E3B341; }
.dark .sev--info    { color: #8B949E; }
.tags { display: flex; flex-wrap: wrap; gap: 6px; }
.tag {
  font-family: var(--vp-font-family-mono);
  font-size: 11.5px;
  padding: 1px 7px;
  border-radius: 4px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-2);
}
</style>
