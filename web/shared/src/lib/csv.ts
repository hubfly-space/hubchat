/** Read a bounded CSV header with quoted commas, escaped quotes, CRLF, and BOM. */
export function parseCSVHeader(input: string): string[] {
 const text=input.replace(/^\uFEFF/,"");const fields:string[]=[];let value="";let quoted=false;
 for(let index=0;index<text.length;index++){
  const char=text[index];
  if(char==='"'){if(quoted && text[index+1]==='"'){value+='"';index++;}else quoted=!quoted;}
  else if(char==="," && !quoted){fields.push(value.trim());if(fields.length>=200)throw new Error("CSV supports at most 200 columns.");value="";}
  else if((char==="\n" ||char==="\r") && !quoted){fields.push(value.trim());return fields;}
  else value+=char;
 }
 if(quoted)throw new Error("The CSV header is incomplete or has an unmatched quote.");
 fields.push(value.trim());return fields;
}
