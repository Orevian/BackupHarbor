// Developer probe: evaluates an expression in the UI served by an already running uidev.
//   node tools/uidev/probe.mjs http://127.0.0.1:8765/ "expression"
import { launchBrowser, sleep } from './cdp.mjs';
const [url, ...exprs] = process.argv.slice(2);
const b = await launchBrowser({ exe: process.env.EDGE || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', port: 9444 });
try {
  await b.goto(url);
  await sleep(1500);
  for (const e of exprs) console.log(JSON.stringify(await b.eval(e)));
  console.log('console:', b.logs.join(' | '));
} finally { b.close(); }
