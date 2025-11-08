(function(){
  const tbody = document.querySelector("#nodes tbody");
  const ts = document.querySelector("#ts");
  const groupSel = document.querySelector("#groupFilter");

  function fmtUptime(sec){
    const d = Math.floor(sec/86400);
    const h = Math.floor((sec%86400)/3600);
    const m = Math.floor((sec%3600)/60);
    return `${d}d ${h}h ${m}m`;
  }
  function maxDiskPct(disks){
    if(!disks || !disks.length) return 0;
    return Math.max.apply(null, disks.map(d => d.used_pct||0));
  }
  function cellAlert(val, thr){
    if(!thr) return "";
    if(typeof val !== "number") return "";
    return val >= thr ? "alert" : "";
  }

  async function refresh(){
    const r = await fetch("/api/latest");
    const rows = await r.json(); // array of {Sample..., Group}
    ts.textContent = new Date().toLocaleTimeString();
    const group = groupSel.value;
    tbody.innerHTML = "";
    rows
      .filter(row => !group || row.group === group)
      .sort((a,b)=> a.node.name.localeCompare(b.node.name))
      .forEach(row => {
        const n = row.node;
        const a = row.agent || {};
        const thr = n.alerts || {};
        const tr = document.createElement("tr");

        const diskMax = maxDiskPct(a.disks);
        const status = row.error ? "DOWN" : "OK";

        tr.innerHTML = `
          <td>${n.name}</td>
          <td>${n.group||""}</td>
          <td class="${cellAlert(a.cpu_percent, thr.cpu_pct)}">${(a.cpu_percent||0).toFixed(1)}</td>
          <td class="${cellAlert(a.mem_percent, thr.mem_pct)}">${(a.mem_percent||0).toFixed(1)}</td>
          <td class="${cellAlert(diskMax, thr.disk_pct)}">${diskMax.toFixed(1)}</td>
          <td class="${cellAlert(a.net?.tx_bytes_per_sec, thr.tx_bps)}">${Math.round(a.net?.tx_bytes_per_sec||0)}</td>
          <td class="${cellAlert(a.net?.rx_bytes_per_sec, thr.rx_bps)}">${Math.round(a.net?.rx_bytes_per_sec||0)}</td>
          <td>${row.latency_ms||"-"}</td>
          <td>${fmtUptime(a.uptime_sec||0)}</td>
          <td class="${row.error?'down':'ok'}">${status}</td>
        `;
        tbody.appendChild(tr);
      });
  }

  setInterval(refresh, 1500);
  refresh();
})();