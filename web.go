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
    input[type=checkbox] { width: auto; }
    .actions { white-space: nowrap; }
    .actions button { width: auto; margin: 0 .25rem 0 0; }
    .ok { color: #16813d; } .bad { color: #c33; }
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
  <h2>Speedtest configuration</h2>
  <p class="hint">Speedtests use Ookla's public server network and consume significant bandwidth. Automatic tests are disabled by default. Review Ookla's <a href="https://www.speedtest.net/about/eula" target="_blank" rel="noopener">EULA</a>, <a href="https://www.speedtest.net/about/terms" target="_blank" rel="noopener">Terms of Use</a>, and <a href="https://www.speedtest.net/about/privacy" target="_blank" rel="noopener">Privacy Policy</a> before accepting.</p>
  <form id="speedtest-config">
    <div class="grid">
      <div><label for="speedtest-interval">Automatic speedtest interval (seconds, minimum 300)</label><input id="speedtest-interval" type="number" min="300" max="604800" required></div>
      <div><label for="speedtest-timeout">Speedtest timeout (seconds, 30–1800)</label><input id="speedtest-timeout" type="number" min="30" max="1800" required></div>
    </div>
    <label><input id="speedtest-terms-accepted" type="checkbox"> I accept Ookla's EULA, Terms of Use, and Privacy Policy for tests run by this exporter</label>
    <label><input id="speedtest-enabled" type="checkbox"> Enable automatic Ookla speedtests</label>
    <button type="submit">Save speedtest configuration</button>
    <p id="speedtest-config-message" role="status"></p>
  </form>
  <button id="reset-config" type="button">Reset all configurations to defaults</button>
  <h2>Latest checks</h2>
  <table><thead><tr><th>Target</th><th>Type</th><th>Status</th><th>Latency</th><th>Jitter</th><th>Packet loss</th></tr></thead><tbody id="results"></tbody></table>
  <h2>Internet speedtest</h2>
  <p class="hint">Use the button to run an on-demand speedtest with the saved speedtest configuration.</p>
  <button id="run-speedtest" type="button">Run speedtest now</button>
  <p id="speedtest-status" role="status">Speedtest has not run yet.</p>
  <script>
    const pingForm = document.querySelector("#ping-config");
    const speedtestForm = document.querySelector("#speedtest-config");
    const pingMessage = document.querySelector("#ping-message");
    const speedtestConfigMessage = document.querySelector("#speedtest-config-message");
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
      document.querySelector("#speedtest-enabled").checked = config.speedtest_enabled;
      document.querySelector("#speedtest-terms-accepted").checked = config.speedtest_terms_accepted;
      document.querySelector("#speedtest-interval").value = config.speedtest_interval_seconds;
      document.querySelector("#speedtest-timeout").value = config.speedtest_timeout_seconds;
    }
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
        speedtestConfigMessage.textContent = "Speedtest settings reset to defaults.";
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
      const buttons = [...pingForm.querySelectorAll("button[type=submit]"), ...speedtestForm.querySelectorAll("button[type=submit]")];
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
    speedtestForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      speedtestConfigMessage.textContent = "Saving speedtest configuration…";
      const changes = {
        speedtest_enabled: document.querySelector("#speedtest-enabled").checked,
        speedtest_terms_accepted: document.querySelector("#speedtest-terms-accepted").checked,
        speedtest_interval_seconds: Number(document.querySelector("#speedtest-interval").value),
        speedtest_timeout_seconds: Number(document.querySelector("#speedtest-timeout").value)
      };
      if (await saveConfigurationChanges(changes, speedtestConfigMessage)) {
        speedtestConfigMessage.textContent = "Speedtest configuration saved.";
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
    function renderSpeedtestStatus(status) {
      const output = document.querySelector("#speedtest-status");
      if (status.running) {
        output.textContent = "Speedtest running… Ping checks continue independently.";
      } else if (!status.attempted) {
        output.textContent = "Speedtest has not run yet.";
      } else if (!status.success) {
        output.textContent = "Last speedtest failed (" + new Date(status.last_run_at).toLocaleString() + "): " + status.error;
      } else {
        const loss = status.packet_loss_percent == null ? "" : ", packet loss " + status.packet_loss_percent.toFixed(1) + "%";
        output.textContent = "Last run " + new Date(status.last_run_at).toLocaleString() + ": download "
          + (status.download_bits_per_second / 1000000).toFixed(2) + " Mbps, upload "
          + (status.upload_bits_per_second / 1000000).toFixed(2) + " Mbps, latency "
          + status.latency_milliseconds.toFixed(2) + " ms" + loss;
      }
      document.querySelector("#run-speedtest").disabled = status.running;
    }
    async function refreshSpeedtestStatus() {
      const response = await fetch("/api/speedtest");
      if (!response.ok) throw new Error("Could not load speedtest status");
      renderSpeedtestStatus(await response.json());
    }
    document.querySelector("#run-speedtest").addEventListener("click", async () => {
      const button = document.querySelector("#run-speedtest");
      button.disabled = true;
      document.querySelector("#speedtest-status").textContent = "Starting speedtest…";
      try {
        const response = await fetch("/api/speedtest/run", {method: "POST"});
        if (!response.ok) throw new Error(await response.text());
        renderSpeedtestStatus(await response.json());
      } catch (error) {
        document.querySelector("#speedtest-status").textContent = error.message;
        button.disabled = false;
      }
    });
    function escapeHTML(value) { return String(value).replace(/[&<>"']/g, (char) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[char])); }
    loadConfig().then(() => Promise.all([refreshResults(), refreshSpeedtestStatus()])).catch((error) => { pingMessage.textContent = error.message; });
    setInterval(() => refreshResults().catch((error) => { pingMessage.textContent = error.message; }), 1000);
    setInterval(() => refreshSpeedtestStatus().catch((error) => { document.querySelector("#speedtest-status").textContent = error.message; }), 1000);
  </script>
</body>
</html>`
