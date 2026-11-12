package main

const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Network Stats Exporter</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
    body { max-width: 900px; margin: 3rem auto; padding: 0 1rem; }
    h1 { margin-bottom: .3rem; }
    .hint, #message { color: #777; }
    label { display: block; margin-top: 1rem; font-weight: 600; }
    input, textarea, button { box-sizing: border-box; width: 100%; padding: .65rem; margin-top: .35rem; font: inherit; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 1rem; }
    button { margin-top: 1.25rem; cursor: pointer; }
    table { width: 100%; border-collapse: collapse; margin-top: 2rem; }
    th, td { padding: .6rem; text-align: left; border-bottom: 1px solid #8885; }
    td input { min-width: 8rem; }
    .actions { white-space: nowrap; }
    .actions button { width: auto; margin: 0 .25rem 0 0; }
    .ok { color: #16813d; } .bad { color: #c33; }
  </style>
</head>
<body>
  <h1>Network Stats Exporter</h1>
  <p class="hint">Configure ICMP ping checks. Prometheus scrapes the results at <code>/metrics</code>.</p>
  <form id="config">
    <h2>Ping targets</h2>
    <p class="hint">Add targets and assign a type such as <code>local</code> or <code>external</code>. Save configuration to apply changes.</p>
    <table><thead><tr><th>Address</th><th>Type</th><th>Actions</th></tr></thead><tbody id="target-list"></tbody></table>
    <button id="add-target" type="button">Add target</button>
    <div class="grid">
      <div><label for="schedule">Schedule (seconds between rounds)</label><input id="schedule" type="number" min="1" max="86400" required></div>
      <div><label for="delay">Delay (milliseconds between targets)</label><input id="delay" type="number" min="0" max="60000" required></div>
      <div><label for="timeout">Timeout (milliseconds per ping)</label><input id="timeout" type="number" min="100" max="60000" required></div>
      <div><label for="packets">Packets per ping check (1–20)</label><input id="packets" type="number" min="1" max="20" required></div>
    </div>
    <button type="submit">Save configuration</button>
    <button id="reset-config" type="button">Reset to defaults</button>
    <p id="message" role="status"></p>
  </form>
  <h2>Latest checks</h2>
  <table><thead><tr><th>Target</th><th>Type</th><th>Status</th><th>Latency</th><th>Packet loss</th></tr></thead><tbody id="results"></tbody></table>
  <script>
    const form = document.querySelector("#config");
    const message = document.querySelector("#message");
    const targetList = document.querySelector("#target-list");
    function addTargetRow(target = {address: "", type: ""}) {
      const row = document.createElement("tr");
      row.innerHTML = "<td><input class='target-address' aria-label='Target address' value='" + escapeHTML(target.address) + "'></td>"
        + "<td><input class='target-type' aria-label='Target type' value='" + escapeHTML(target.type) + "'></td>"
        + "<td class='actions'><button type='button' class='remove-target'>Remove</button></td>";
      targetList.appendChild(row);
    }
    function readTargets() {
      return [...targetList.querySelectorAll("tr")].map((row) => ({
        address: row.querySelector(".target-address").value.trim(),
        type: row.querySelector(".target-type").value.trim()
      })).filter((target) => target.address);
    }
    function renderConfig(config) {
      targetList.replaceChildren();
      config.targets.forEach(addTargetRow);
      document.querySelector("#schedule").value = config.schedule_seconds;
      document.querySelector("#delay").value = config.delay_milliseconds;
      document.querySelector("#timeout").value = config.timeout_milliseconds;
      document.querySelector("#packets").value = config.packets_per_ping;
    }
    document.querySelector("#add-target").addEventListener("click", () => addTargetRow());
    targetList.addEventListener("click", (event) => {
      if (event.target.closest(".remove-target")) event.target.closest("tr").remove();
    });
    document.querySelector("#reset-config").addEventListener("click", async () => {
      if (!confirm("Reset all ping settings and targets to defaults?")) return;
      message.textContent = "Resetting configuration…";
      try {
        const response = await fetch("/api/config/reset", {method: "POST"});
        if (!response.ok) throw new Error(await response.text());
        renderConfig(await response.json());
        message.textContent = "Configuration reset to defaults and saved.";
      } catch (error) { message.textContent = error.message; }
    });
    async function loadConfig() {
      const response = await fetch("/api/config");
      if (!response.ok) throw new Error("Could not load configuration");
      renderConfig(await response.json());
    }
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      message.textContent = "Saving…";
      const config = {
        targets: readTargets(),
        schedule_seconds: Number(document.querySelector("#schedule").value),
        delay_milliseconds: Number(document.querySelector("#delay").value),
        timeout_milliseconds: Number(document.querySelector("#timeout").value),
        packets_per_ping: Number(document.querySelector("#packets").value)
      };
      try {
        const response = await fetch("/api/config", {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify(config)});
        if (!response.ok) throw new Error(await response.text());
        targetList.replaceChildren();
        config.targets.forEach(addTargetRow);
        message.textContent = "Configuration saved. It will be used on the next round.";
      } catch (error) { message.textContent = error.message; }
    });
    async function refreshResults() {
      const response = await fetch("/metrics");
      if (!response.ok) throw new Error("Could not load ping results");
      const metrics = await response.text();
      function valuesFor(metric) {
        return new Map([...metrics.matchAll(new RegExp("^" + metric + "\\{target=\"([^\"]+)\",type=\"([^\"]+)\"\\} ([\\d.e+-]+)$", "gm"))]
          .map((match) => [match[1], Number(match[3])]));
      }
      const success = valuesFor("network_ping_success");
      const latency = valuesFor("network_ping_duration_seconds");
      const packetLoss = valuesFor("network_ping_packet_loss_ratio");
      document.querySelector("#results").innerHTML = readTargets().map((target) => {
        const ok = success.has(target.address) ? success.get(target.address) === 1 : undefined;
        const status = ok === undefined ? "Waiting for first check" : ok ? "Reachable" : "Unreachable";
        const ms = latency.has(target.address) ? (latency.get(target.address) * 1000).toFixed(2) + " ms" : "—";
        const loss = packetLoss.has(target.address) ? (packetLoss.get(target.address) * 100).toFixed(0) + "%" : "—";
        const statusClass = ok === true ? "ok" : ok === false ? "bad" : "";
        return "<tr><td>" + escapeHTML(target.address) + "</td><td>" + escapeHTML(target.type)
          + "</td><td class='" + statusClass + "'>" + escapeHTML(status) + "</td><td>" + escapeHTML(ms) + "</td><td>"
          + escapeHTML(loss) + "</td></tr>";
      }).join("");
    }
    function escapeHTML(value) { return String(value).replace(/[&<>"']/g, (char) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[char])); }
    loadConfig().then(refreshResults).catch((error) => { message.textContent = error.message; });
    setInterval(() => refreshResults().catch((error) => { message.textContent = error.message; }), 1000);
  </script>
</body>
</html>`
