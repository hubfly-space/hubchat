import { readFileSync, existsSync } from "node:fs";
import { dirname, resolve, relative } from "node:path";
import { gzipSync } from "node:zlib";

const root = resolve("embedded/assets/widget");
const seen = new Set();
function initialGraph(file) {
 const absolute=resolve(file);
 if(seen.has(absolute))return 0;
 if(relative(root,absolute).startsWith(".."))throw new Error("Widget imports a file outside its asset directory");
 if(!existsSync(absolute))throw new Error(`Missing widget asset: ${relative(root,absolute)}`);
 seen.add(absolute);
 const source=readFileSync(absolute,"utf8");
 let bytes=gzipSync(source,{level:9}).length;
 // Static imports only. Optional import() chunks are charged separately.
 for(const match of source.matchAll(/\b(?:import|export)\s*(?:[^"';]*?\bfrom\s*)?["'](\.\.?\/[^"']+)["']/g)) bytes+=initialGraph(resolve(dirname(absolute),match[1]));
 return bytes;
}
const interfaceBytes=initialGraph(resolve(root,"app.js"));
const loaderBytes=gzipSync(readFileSync(resolve(root,"v1.js")),{level:9}).length;
console.log(`Widget interface: ${(interfaceBytes/1024).toFixed(1)} KiB gzip (${seen.size} initial modules); SDK loader: ${(loaderBytes/1024).toFixed(1)} KiB gzip`);
if(interfaceBytes>70*1024)throw new Error("Widget interface exceeds its 70 KiB gzip budget");
if(loaderBytes>15*1024)throw new Error("Widget SDK loader exceeds its 15 KiB gzip budget");
if(!/\bmount\b/.test(readFileSync(resolve(root,"app.js"),"utf8")))throw new Error("Widget mount export is absent");
