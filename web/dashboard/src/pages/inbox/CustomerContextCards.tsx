import { api, useQuery, type Customer, type Paginated } from "@hubchat/shared";
type Field = {key:string;label:string;format?:string;currency?:string};
type Card = {id:string;title:string;fields:Field[]};
function text(value:unknown):string {
 if(value===null || value===undefined)return "—";
 if(Array.isArray(value))return value.slice(0,5).map(text).join(" · ");
 if(typeof value==="object")return Object.values(value).slice(0,6).map(text).join(" · ");
 return String(value);
}
function render(value:unknown,field:Field) {
 if(value==="[REDACTED]" || value===null || value===undefined)return text(value);
 if(field.format==="date"){const date=new Date(String(value));return Number.isNaN(date.getTime())?text(value):date.toLocaleDateString();}
 if(field.format==="currency" && typeof value==="number"){try{return new Intl.NumberFormat(undefined,{style:"currency",currency:field.currency||"USD"}).format(value);}catch{return text(value);}}
 if(field.format==="link" && typeof value==="string"){try{const url=new URL(value);if(url.protocol==="https:" || url.protocol==="http:")return <a href={url.href} target="_blank" rel="noopener noreferrer" className="text-accent-text">Open {field.label||"record"}</a>;}catch{/* display as text */}}
 return text(value);
}
export function CustomerContextCards({customer}:{customer:Customer}) {
 const cards=useQuery<Paginated<Card>>(["customer-context-cards"],signal=>api.get("/customer-context/cards",{signal}));
 return <>{cards.data?.data.map(card=><section key={card.id} className="border-b border-line p-4"><h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-fg-muted">{card.title}</h3><dl className="space-y-2">{card.fields.map(field=><div key={field.key} className="flex items-start justify-between gap-3 text-xs"><dt className="text-fg-muted">{field.label||field.key}</dt><dd className="max-w-[65%] break-words text-right">{render(customer.attributes[field.key],field)}</dd></div>)}</dl></section>)}</>;
}
