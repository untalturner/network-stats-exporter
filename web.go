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
    input, textarea, button, select { box-sizing: border-box; width: 100%; padding: .65rem; margin-top: .35rem; font: inherit; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 1rem; }
    button { margin-top: 1.25rem; cursor: pointer; }
    table { width: 100%; border-collapse: collapse; margin-top: 2rem; }
    th, td { padding: .6rem; text-align: left; border-bottom: 1px solid #8885; }
    td input { min-width: 8rem; }
    input[type=checkbox] { width: auto; }
    .actions { white-space: nowrap; }
    .actions button { width: auto; margin: 0 .25rem 0 0; }
    .ok { color: #16813d; } .bad { color: #c33; }
    [hidden] { display: none !important; }
  </style>
</head>
<body>
  <h1>Network Stats Exporter</h1>
  <p class="hint">Configure ping checks and speedtests independently. Prometheus scrapes the results at <code>/metrics</code>.</p>
  <form id="ping-config">
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
    <button type="submit">Save ping configuration</button>
    <p id="ping-message" role="status"></p>
  </form>
  <h2>Latest ping checks</h2>
  <table><thead><tr><th>Target</th><th>Type</th><th>Status</th><th>Latency</th><th>Jitter</th><th>Packet loss</th></tr></thead><tbody id="results"></tbody></table>
  <h2>Speedtests</h2>
  <p class="hint">Configure and schedule each provider independently. Both may run at the same time. Cron expressions use five fields and the server's local timezone. Ookla controls its own test workload; concurrency and transfer-size settings apply only to Cloudflare.</p>
  <div class="grid">
    <div><label for="speedtest-timeout">Speedtest timeout (seconds, 30–1800)</label><input id="speedtest-timeout" type="number" min="30" max="1800" required></div>
  </div>
  <form id="ookla-config">
    <h3>Ookla Speedtest</h3>
    <p class="hint">Uses Ookla's public server network. Review its <a href="https://www.speedtest.net/about/eula" target="_blank" rel="noopener">EULA</a>, <a href="https://www.speedtest.net/about/terms" target="_blank" rel="noopener">Terms of Use</a>, and <a href="https://www.speedtest.net/about/privacy" target="_blank" rel="noopener">Privacy Policy</a>.</p>
    <label><input id="ookla-enabled" type="checkbox"> Enable scheduled Ookla tests</label>
    <div class="grid">
      <div><label for="ookla-interval">Interval in seconds (300–604800; ignored when cron is set)</label><input id="ookla-interval" type="number" min="300" max="604800" required></div>
      <div><label for="ookla-cron">Cron schedule (optional, e.g. <code>0 */6 * * *</code>)</label><input id="ookla-cron" type="text" placeholder="Leave blank to use interval"></div>
    </div>
    <label><input id="speedtest-terms-accepted" type="checkbox"> I accept Ookla's EULA, Terms of Use, and Privacy Policy for tests run by this exporter</label>
    <button type="submit">Save Ookla settings</button>
    <button id="run-ookla" type="button">Run Ookla now</button>
    <p id="ookla-message" role="status">Ookla speedtest has not run yet.</p>
    <p id="ookla-next-run" class="hint" role="status">Automatic schedule disabled.</p>
  </form>
  <form id="cloudflare-config">
    <h3>Cloudflare edge network</h3>
    <p class="hint">Test requests share the exporter host's public IP address with Cloudflare and are subject to its <a href="https://www.cloudflare.com/privacypolicy/" target="_blank" rel="noopener">Privacy Policy</a>.</p>
    <label><input id="cloudflare-enabled" type="checkbox"> Enable scheduled Cloudflare tests</label>
    <div class="grid">
      <div><label for="cloudflare-interval">Interval in seconds (300–604800; ignored when cron is set)</label><input id="cloudflare-interval" type="number" min="300" max="604800" required></div>
      <div><label for="cloudflare-cron">Cron schedule (optional, e.g. <code>15 */6 * * *</code>)</label><input id="cloudflare-cron" type="text" placeholder="Leave blank to use interval"></div>
      <div><label for="cloudflare-concurrent-requests">Concurrent requests per direction (1–8)</label><input id="cloudflare-concurrent-requests" type="number" min="1" max="8" required></div>
      <div><label for="cloudflare-request-size-mb">Size per request (MB, 1–50)</label><input id="cloudflare-request-size-mb" type="number" min="1" max="50" required></div>
    </div>
    <p id="cloudflare-transfer-estimate" class="hint"></p>
    <label><input id="speedtest-cloudflare-notice-accepted" type="checkbox"> I understand this test sends requests to Cloudflare and shares the public IP address</label>
    <button type="submit">Save Cloudflare settings</button>
    <button id="run-cloudflare" type="button">Run Cloudflare now</button>
    <p id="cloudflare-message" role="status">Cloudflare speedtest has not run yet.</p>
    <p id="cloudflare-next-run" class="hint" role="status">Automatic schedule disabled.</p>
  </form>
  <button id="reset-config" type="button">Reset all configurations to defaults</button>
  <script>
    const pingForm = document.querySelector("#ping-config");
    const ooklaForm = document.querySelector("#ookla-config");
    const cloudflareForm = document.querySelector("#cloudflare-config");
    const pingMessage = document.querySelector("#ping-message");
    const ooklaMessage = document.querySelector("#ookla-message");
    const cloudflareMessage = document.querySelector("#cloudflare-message");
    const targetList = document.querySelector("#target-list");
    let currentConfig;
    let configSaveInProgress = false;
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
      currentConfig = config;
      targetList.replaceChildren();
      config.targets.forEach(addTargetRow);
      document.querySelector("#schedule").value = config.schedule_seconds;
      document.querySelector("#delay").value = config.delay_milliseconds;
      document.querySelector("#timeout").value = config.timeout_milliseconds;
      document.querySelector("#packets").value = config.packets_per_ping;
      document.querySelector("#ookla-enabled").checked = config.speedtest_ookla_enabled;
      document.querySelector("#ookla-interval").value = config.speedtest_ookla_interval_seconds || 1800;
      document.querySelector("#ookla-cron").value = config.speedtest_ookla_cron || "";
      document.querySelector("#cloudflare-enabled").checked = config.speedtest_cloudflare_enabled;
      document.querySelector("#cloudflare-interval").value = config.speedtest_cloudflare_interval_seconds || 1800;
      document.querySelector("#cloudflare-cron").value = config.speedtest_cloudflare_cron || "";
      document.querySelector("#speedtest-terms-accepted").checked = config.speedtest_terms_accepted;
      document.querySelector("#speedtest-cloudflare-notice-accepted").checked = config.speedtest_cloudflare_notice_accepted;
      document.querySelector("#speedtest-timeout").value = config.speedtest_timeout_seconds;
      document.querySelector("#cloudflare-concurrent-requests").value = config.cloudflare_concurrent_requests || 4;
      document.querySelector("#cloudflare-request-size-mb").value = config.cloudflare_request_size_mb || 5;
      updateCloudflareEstimate();
    }
    function updateCloudflareEstimate() {
      const concurrency = Number(document.querySelector("#cloudflare-concurrent-requests").value) || 0;
      const sizeMB = Number(document.querySelector("#cloudflare-request-size-mb").value) || 0;
      document.querySelector("#cloudflare-transfer-estimate").textContent =
        "Approximate maximum per test: " + (2 * concurrency * sizeMB) + " MB total (download + upload).";
    }
    document.querySelector("#cloudflare-concurrent-requests").addEventListener("input", updateCloudflareEstimate);
    document.querySelector("#cloudflare-request-size-mb").addEventListener("input", updateCloudflareEstimate);
    document.querySelector("#add-target").addEventListener("click", () => addTargetRow());
    targetList.addEventListener("click", (event) => {
      if (event.target.closest(".remove-target")) event.target.closest("tr").remove();
    });
    document.querySelector("#reset-config").addEventListener("click", async () => {
      if (!confirm("Reset all ping and speedtest settings and targets to defaults?")) return;
      pingMessage.textContent = "Resetting all configuration…";
      try {
        const response = await fetch("/api/config/reset", {method: "POST"});
        if (!response.ok) throw new Error(await response.text());
        renderConfig(await response.json());
        pingMessage.textContent = "All configuration reset to defaults and saved.";
        ooklaMessage.textContent = "Ookla settings reset to defaults.";
        cloudflareMessage.textContent = "Cloudflare settings reset to defaults.";
      } catch (error) { pingMessage.textContent = error.message; }
    });
    async function loadConfig() {
      const response = await fetch("/api/config");
      if (!response.ok) throw new Error("Could not load configuration");
      renderConfig(await response.json());
    }
    async function saveConfigurationChanges(changes, statusElement) {
      if (configSaveInProgress) return false;
      configSaveInProgress = true;
      const buttons = [...pingForm.querySelectorAll("button[type=submit]"), ...ooklaForm.querySelectorAll("button[type=submit]"), ...cloudflareForm.querySelectorAll("button[type=submit]")];
      buttons.forEach((button) => { button.disabled = true; });
      try {
        const getResponse = await fetch("/api/config");
        if (!getResponse.ok) throw new Error(await getResponse.text());
        const latestConfig = await getResponse.json();
        const config = {...latestConfig, ...changes};
        const response = await fetch("/api/config", {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify(config)});
        if (!response.ok) throw new Error(await response.text());
        currentConfig = await response.json();
        return true;
      } catch (error) {
        statusElement.textContent = error.message;
        return false;
      } finally {
        configSaveInProgress = false;
        buttons.forEach((button) => { button.disabled = false; });
      }
    }
    pingForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      pingMessage.textContent = "Saving ping configuration…";
      const changes = {
        targets: readTargets(),
        schedule_seconds: Number(document.querySelector("#schedule").value),
        delay_milliseconds: Number(document.querySelector("#delay").value),
        timeout_milliseconds: Number(document.querySelector("#timeout").value),
        packets_per_ping: Number(document.querySelector("#packets").value)
      };
      if (await saveConfigurationChanges(changes, pingMessage)) {
        targetList.replaceChildren();
        currentConfig.targets.forEach(addTargetRow);
        pingMessage.textContent = "Ping configuration saved. It will be used on the next round.";
      }
    });
    ooklaForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      ooklaMessage.textContent = "Saving Ookla settings…";
      const changes = {
        speedtest_enabled: false,
        speedtest_ookla_enabled: document.querySelector("#ookla-enabled").checked,
        speedtest_ookla_interval_seconds: Number(document.querySelector("#ookla-interval").value),
        speedtest_ookla_cron: document.querySelector("#ookla-cron").value.trim(),
        speedtest_terms_accepted: document.querySelector("#speedtest-terms-accepted").checked,
        speedtest_timeout_seconds: Number(document.querySelector("#speedtest-timeout").value)
      };
      if (await saveConfigurationChanges(changes, ooklaMessage)) {
        ooklaMessage.textContent = "Ookla settings saved.";
      }
    });
    cloudflareForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      cloudflareMessage.textContent = "Saving Cloudflare settings…";
      const changes = {
        speedtest_enabled: false,
        speedtest_cloudflare_enabled: document.querySelector("#cloudflare-enabled").checked,
        speedtest_cloudflare_interval_seconds: Number(document.querySelector("#cloudflare-interval").value),
        speedtest_cloudflare_cron: document.querySelector("#cloudflare-cron").value.trim(),
        speedtest_cloudflare_notice_accepted: document.querySelector("#speedtest-cloudflare-notice-accepted").checked,
        speedtest_timeout_seconds: Number(document.querySelector("#speedtest-timeout").value),
        cloudflare_concurrent_requests: Number(document.querySelector("#cloudflare-concurrent-requests").value),
        cloudflare_request_size_mb: Number(document.querySelector("#cloudflare-request-size-mb").value)
      };
      if (await saveConfigurationChanges(changes, cloudflareMessage)) {
        cloudflareMessage.textContent = "Cloudflare settings saved.";
      }
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
      const jitter = valuesFor("network_ping_jitter_seconds");
      const packetLoss = valuesFor("network_ping_packet_loss_ratio");
      document.querySelector("#results").innerHTML = readTargets().map((target) => {
        const ok = success.has(target.address) ? success.get(target.address) === 1 : undefined;
        const status = ok === undefined ? "Waiting for first check" : ok ? "Reachable" : "Unreachable";
        const ms = latency.has(target.address) ? (latency.get(target.address) * 1000).toFixed(2) + " ms" : "—";
        const jitterMs = jitter.has(target.address) ? (jitter.get(target.address) * 1000).toFixed(2) + " ms" : "—";
        const loss = packetLoss.has(target.address) ? (packetLoss.get(target.address) * 100).toFixed(0) + "%" : "—";
        const statusClass = ok === true ? "ok" : ok === false ? "bad" : "";
        return "<tr><td>" + escapeHTML(target.address) + "</td><td>" + escapeHTML(target.type)
          + "</td><td class='" + statusClass + "'>" + escapeHTML(status) + "</td><td>" + escapeHTML(ms) + "</td><td>"
          + escapeHTML(jitterMs) + "</td><td>"
          + escapeHTML(loss) + "</td></tr>";
      }).join("");
    }
    function renderSpeedtestStatus(provider, status) {
      const output = provider === "cloudflare" ? cloudflareMessage : ooklaMessage;
      const button = document.querySelector(provider === "cloudflare" ? "#run-cloudflare" : "#run-ookla");
      const providerName = provider === "cloudflare" ? "Cloudflare" : "Ookla";
      const nextRunOutput = document.querySelector(provider === "cloudflare" ? "#cloudflare-next-run" : "#ookla-next-run");
      const enabled = provider === "cloudflare" ? currentConfig.speedtest_cloudflare_enabled : currentConfig.speedtest_ookla_enabled;
      const cron = provider === "cloudflare" ? currentConfig.speedtest_cloudflare_cron : currentConfig.speedtest_ookla_cron;
      const scheduleType = cron ? "cron" : "interval";
      if (!enabled) {
        nextRunOutput.textContent = "Automatic schedule disabled.";
      } else if (status.next_run_at) {
        nextRunOutput.textContent = "Next automatic execution (" + scheduleType + "): "
          + new Date(status.next_run_at).toLocaleString();
      } else {
        nextRunOutput.textContent = "Next automatic execution (" + scheduleType + "): calculating…";
      }
      if (status.running) {
        output.textContent = providerName + " speedtest running… Ping checks continue independently.";
      } else if (!status.attempted) {
        output.textContent = providerName + " speedtest has not run yet.";
      } else if (!status.success) {
        output.textContent = "Last " + providerName + " speedtest failed (" + new Date(status.last_run_at).toLocaleString() + "): " + status.error;
      } else {
        const loss = status.packet_loss_percent == null ? "" : ", packet loss " + status.packet_loss_percent.toFixed(1) + "%";
        output.textContent = "Last " + providerName + " run " + new Date(status.last_run_at).toLocaleString() + ": download "
          + (status.download_bits_per_second / 1000000).toFixed(2) + " Mbps, upload "
          + (status.upload_bits_per_second / 1000000).toFixed(2) + " Mbps, latency "
          + status.latency_milliseconds.toFixed(2) + " ms" + loss;
      }
      button.disabled = status.running;
    }
    async function refreshSpeedtestStatus() {
      const response = await fetch("/api/speedtest");
      if (!response.ok) throw new Error("Could not load speedtest status");
      const statuses = await response.json();
      renderSpeedtestStatus("ookla", statuses.ookla || {});
      renderSpeedtestStatus("cloudflare", statuses.cloudflare || {});
    }
    async function runSpeedtest(provider) {
      const button = document.querySelector(provider === "cloudflare" ? "#run-cloudflare" : "#run-ookla");
      const output = provider === "cloudflare" ? cloudflareMessage : ooklaMessage;
      button.disabled = true;
      output.textContent = "Starting " + (provider === "cloudflare" ? "Cloudflare" : "Ookla") + " speedtest…";
      try {
        const response = await fetch("/api/speedtest/run?provider=" + provider, {method: "POST"});
        if (!response.ok) throw new Error(await response.text());
        renderSpeedtestStatus(provider, await response.json());
      } catch (error) {
        output.textContent = error.message;
        button.disabled = false;
      }
    }
    document.querySelector("#run-ookla").addEventListener("click", () => runSpeedtest("ookla"));
    document.querySelector("#run-cloudflare").addEventListener("click", () => runSpeedtest("cloudflare"));
    function escapeHTML(value) { return String(value).replace(/[&<>"']/g, (char) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[char])); }
    loadConfig().then(() => Promise.all([refreshResults(), refreshSpeedtestStatus()])).catch((error) => { pingMessage.textContent = error.message; });
    setInterval(() => refreshResults().catch((error) => { pingMessage.textContent = error.message; }), 1000);
    setInterval(() => refreshSpeedtestStatus().catch((error) => { ooklaMessage.textContent = error.message; cloudflareMessage.textContent = error.message; }), 1000);
  </script>
</body>
</html>`
