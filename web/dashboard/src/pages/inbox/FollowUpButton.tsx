import { api, Button, Dialog, DialogContent, DialogTrigger, Field, Input, Textarea, Callout, useMutation, useQuery, invalidate, idempotencyKey, parseDateTimeLocal, formatDateTime, type Paginated } from "@hubchat/shared";
import { Clock } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
type FollowUp={id:string;body:string;scheduled_for:string;state:string};
export function FollowUpButton({conversationID}:{conversationID:string}) {
 const {workspace}=useWorkspace();const [open,setOpen]=useState(false);const [body,setBody]=useState("");const [at,setAt]=useState("");
 const list=useQuery<Paginated<FollowUp>>(open?["follow-ups",conversationID]:null,signal=>api.get(`/conversations/${conversationID}/follow-ups`,{signal}));
 const create=useMutation<void,FollowUp>(()=>api.post(`/conversations/${conversationID}/follow-ups`,{body,scheduled_for:parseDateTimeLocal(at,workspace.timezone)},{idempotencyKey:idempotencyKey()}),{onSuccess:()=>{invalidate(["follow-ups",conversationID]);setBody("");setAt("");}});
 const cancel=useMutation<string,unknown>(id=>api.delete(`/conversations/${conversationID}/follow-ups/${id}`),{invalidates:[["follow-ups",conversationID]]});
 return <Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button variant="ghost" size="sm" iconOnly leading={<Clock/>} aria-label="Schedule follow-up"/></DialogTrigger><DialogContent title="Follow-up reminders" description="Private reminders for this conversation. Delivery uses your assignment notification preferences.">
  <form className="space-y-3" onSubmit={event=>{event.preventDefault();void create.mutate(undefined).catch(()=>{});}}><Field label="Reminder"><Textarea required value={body} onChange={event=>setBody(event.target.value)} rows={3}/></Field><Field label="Remind me at" description={`Uses ${workspace.timezone}.`}><Input required type="datetime-local" value={at} onChange={event=>setAt(event.target.value)}/></Field><Button type="submit" loading={create.isPending} disabled={!body.trim() ||!parseDateTimeLocal(at,workspace.timezone)}>Schedule reminder</Button></form>
  {Boolean(create.error || cancel.error || list.error) && <Callout tone="danger">{(create.error || cancel.error || list.error) instanceof Error ? ((create.error || cancel.error || list.error) as Error).message:"Could not update reminders."}</Callout>}
  <ul className="mt-4 space-y-2">{list.data?.data.map(item=><li key={item.id} className="rounded border border-line p-3"><p className="text-sm">{item.body}</p><time className="text-xs text-fg-muted">{formatDateTime(item.scheduled_for,{timeZone:workspace.timezone})}</time><Button variant="ghost" size="sm" disabled={item.state!=="pending"} onClick={()=>void cancel.mutate(item.id).catch(()=>{})}>Cancel</Button></li>)}</ul>
 </DialogContent></Dialog>;
}
