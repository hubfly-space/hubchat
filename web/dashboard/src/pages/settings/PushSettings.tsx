import { api, pushSubscriptionKey, Button, Callout, useQuery, useMutation } from "@hubchat/shared";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
const keyBytes=(value:string)=>Uint8Array.from(atob(value.replace(/-/g,"+").replace(/_/g,"/").padEnd(Math.ceil(value.length/4)*4,"=")),char=>char.charCodeAt(0));
export function PushSettings() {
 const {workspace,viewer}=useWorkspace();const storageKey=pushSubscriptionKey(viewer.id,workspace.id);
 const [subscriptionID,setSubscriptionID]=useState(()=>{try{return localStorage.getItem(storageKey)??"";}catch{return "";}});
 const config=useQuery<{enabled:boolean;public_key:string}>(["push-config"],signal=>api.get("/notifications/push/config",{signal}));
 const supported=typeof window!=="undefined" && window.isSecureContext && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
 const enable=useMutation<void,void>(async()=>{
  if(!supported || !config.data?.enabled)throw new Error("Push is unavailable on this device or server.");
  const permission=await Notification.requestPermission();if(permission!=="granted")throw new Error("Allow notifications in your browser settings to enable push.");
  const registration=await navigator.serviceWorker.register("/app/sw.js",{scope:"/app/"});await navigator.serviceWorker.ready;
  let subscription=await registration.pushManager.getSubscription();
  const key=keyBytes(config.data.public_key);
  if(subscription?.options.applicationServerKey){const existing=new Uint8Array(subscription.options.applicationServerKey);if(existing.length!==key.length || existing.some((value,index)=>value!==key[index])){await subscription.unsubscribe();subscription=null;}}
  subscription??=await registration.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:key.buffer});
  const response=await api.post<{id:string}>("/notifications/push/subscriptions",subscription.toJSON());
  setSubscriptionID(response.id);try{localStorage.setItem(storageKey,response.id);}catch{/* optional */}
 });
 const disable=useMutation<void,void>(async()=>{
  if(subscriptionID)await api.delete(`/notifications/push/subscriptions/${subscriptionID}`);
  try{localStorage.removeItem(storageKey);}catch{/* optional */}setSubscriptionID("");
 });
 return <div className="mt-4 rounded border border-line p-4"><h3 className="text-sm font-medium">Background push notifications</h3><p className="mt-1 text-xs text-fg-muted">Receive the events enabled in the Browser column when the dashboard is closed. On iPhone or iPad, add Hubchat to your Home Screen first.</p>
  {!supported ? <p className="mt-2 text-xs text-fg-muted">This browser needs a secure connection and Web Push support.</p> : !config.data?.enabled ? <p className="mt-2 text-xs text-fg-muted">Push delivery is not enabled on this server. Ask your administrator to enable it.</p> : <Button className="mt-3" variant="secondary" size="sm" loading={enable.isPending || disable.isPending} onClick={()=>void (subscriptionID?disable:enable).mutate(undefined).catch(()=>{})}>{subscriptionID?"Disable push on this device":"Enable push on this device"}</Button>}
  {Boolean(enable.error || disable.error) && <Callout tone="danger" className="mt-2">{(enable.error || disable.error) instanceof Error?((enable.error || disable.error) as Error).message:"Could not update push notifications."}</Callout>}
 </div>;
}
