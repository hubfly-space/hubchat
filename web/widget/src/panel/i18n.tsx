import { createContext, useContext, useMemo, type ReactNode } from "react";

type MessageTable = Record<string, string>;
type Values = Record<string, string | number>;

const messages: Record<string, MessageTable> = {
  en: {
    open_support: "Open support", close_support: "Close support", back: "Back", minimise: "Minimise", offline: "Offline",
    help_articles: "Help articles", guide_loading: "Loading guide…", guide_unavailable: "This guide is no longer available.", start_conversation: "Start a conversation", leave_message: "Leave a message", search_help: "Search for help",
    submit_request: "Submit a detailed request", share_feedback: "Share feedback", sending: "Sending…", sent: "Sent", failed_retry: "Failed — tap to retry",
    agent_typing: "Agent is typing", search_articles: "Search articles", articles_unavailable: "Articles are unavailable right now. Try again in a moment.",
    nothing_matched: "Nothing matched. Try fewer words, or start a conversation.", load_more_articles: "Load more articles", loading: "Loading…",
    feedback_unavailable: "Feedback is unavailable.", load_more_feedback_boards: "Load more boards", load_more_feedback: "Load more feedback",
    no_public_feedback: "No public feedback boards", no_feedback: "No feedback here yet", improve: "What should we improve?", feedback_why: "Tell us why this matters…",
    submit_feedback: "Submit feedback", voted: "Voted", vote: "Vote · {count}", following: "Following", follow: "Follow", identify_to_vote: "Identify yourself before voting.",
    identify_to_follow: "Identify yourself before following feedback.", request_forms_unavailable: "Forms are unavailable.", load_more_forms: "Load more forms",
    request_received: "Request received", request_sent: "Your submission was sent to the support team.", no_request_forms: "No request forms are available",
    start_conversation_instead: "Start a conversation instead and a person will help you there.", choose: "Choose…", uploading: "Uploading…", file_ready: "File ready",
    send_request: "Send request", attachment_sent_error: "Your message was sent, but an attachment could not be uploaded.", message: "Message", attach_file: "Attach a file",
    powered_by: "Powered by Hubchat", you: "You", attachment_upload_error: "The file could not be uploaded.", could_not_load_feedback: "Could not load feedback items.",
  },
  fr: {
    open_support: "Ouvrir l’assistance", close_support: "Fermer l’assistance", back: "Retour", minimise: "Réduire", offline: "Hors ligne",
    help_articles: "Articles d’aide", guide_loading: "Chargement du guide…", guide_unavailable: "Ce guide n’est plus disponible.", start_conversation: "Démarrer une conversation", leave_message: "Laisser un message", search_help: "Rechercher de l’aide",
    submit_request: "Envoyer une demande détaillée", share_feedback: "Partager un retour", sending: "Envoi…", sent: "Envoyé", failed_retry: "Échec — toucher pour réessayer",
    agent_typing: "Un agent écrit", search_articles: "Rechercher des articles", articles_unavailable: "Articles indisponibles. Réessayez dans un instant.",
    nothing_matched: "Aucun résultat. Essayez moins de mots ou démarrez une conversation.", load_more_articles: "Charger plus d’articles", loading: "Chargement…",
    feedback_unavailable: "Retours indisponibles.", load_more_feedback_boards: "Charger plus de tableaux", load_more_feedback: "Charger plus de retours",
    no_public_feedback: "Aucun tableau de retours public", no_feedback: "Aucun retour ici pour le moment", improve: "Que devons-nous améliorer ?", feedback_why: "Dites-nous pourquoi c’est important…",
    submit_feedback: "Envoyer le retour", voted: "Voté", vote: "Voter · {count}", following: "Suivi", follow: "Suivre", identify_to_vote: "Identifiez-vous avant de voter.",
    identify_to_follow: "Identifiez-vous avant de suivre un retour.", request_forms_unavailable: "Formulaires indisponibles.", load_more_forms: "Charger plus de formulaires",
    request_received: "Demande reçue", request_sent: "Votre demande a été envoyée à l’équipe d’assistance.", no_request_forms: "Aucun formulaire de demande disponible",
    start_conversation_instead: "Démarrez plutôt une conversation et une personne vous aidera.", choose: "Choisir…", uploading: "Envoi…", file_ready: "Fichier prêt",
    send_request: "Envoyer la demande", attachment_sent_error: "Votre message a été envoyé, mais une pièce jointe n’a pas pu être envoyée.", message: "Message", attach_file: "Joindre un fichier",
    powered_by: "Propulsé par Hubchat", you: "Vous", attachment_upload_error: "Le fichier n’a pas pu être envoyé.", could_not_load_feedback: "Impossible de charger les retours.",
  },
  es: {
    open_support: "Abrir soporte", close_support: "Cerrar soporte", back: "Atrás", minimise: "Minimizar", offline: "Sin conexión", help_articles: "Artículos de ayuda",
    guide_loading: "Cargando guía…", guide_unavailable: "Esta guía ya no está disponible.", start_conversation: "Iniciar una conversación", leave_message: "Dejar un mensaje", search_help: "Buscar ayuda", submit_request: "Enviar una solicitud detallada", share_feedback: "Compartir comentarios",
    sending: "Enviando…", sent: "Enviado", failed_retry: "Falló — toca para reintentar", agent_typing: "El agente está escribiendo", search_articles: "Buscar artículos",
    articles_unavailable: "Los artículos no están disponibles. Inténtalo de nuevo en un momento.", nothing_matched: "No hay coincidencias. Prueba con menos palabras o inicia una conversación.",
    load_more_articles: "Cargar más artículos", loading: "Cargando…", feedback_unavailable: "Los comentarios no están disponibles.", load_more_feedback_boards: "Cargar más tableros", load_more_feedback: "Cargar más comentarios",
    no_public_feedback: "No hay tableros públicos", no_feedback: "Aún no hay comentarios", improve: "¿Qué deberíamos mejorar?", feedback_why: "Cuéntanos por qué es importante…", submit_feedback: "Enviar comentario",
    voted: "Votado", vote: "Votar · {count}", following: "Siguiendo", follow: "Seguir", identify_to_vote: "Identifícate antes de votar.", identify_to_follow: "Identifícate antes de seguir comentarios.",
    request_forms_unavailable: "Los formularios no están disponibles.", load_more_forms: "Cargar más formularios", request_received: "Solicitud recibida", request_sent: "Tu solicitud se envió al equipo de soporte.", no_request_forms: "No hay formularios disponibles",
    start_conversation_instead: "Inicia una conversación y una persona te ayudará.", choose: "Elegir…", uploading: "Subiendo…", file_ready: "Archivo listo", send_request: "Enviar solicitud",
    attachment_sent_error: "Tu mensaje se envió, pero no se pudo subir un archivo.", message: "Mensaje", attach_file: "Adjuntar archivo", powered_by: "Desarrollado por Hubchat", you: "Tú", attachment_upload_error: "No se pudo subir el archivo.", could_not_load_feedback: "No se pudieron cargar los comentarios.",
  },
  ar: {
    open_support: "فتح الدعم", close_support: "إغلاق الدعم", back: "رجوع", minimise: "تصغير", offline: "غير متصل", help_articles: "مقالات المساعدة",
    guide_loading: "جار تحميل الدليل…", guide_unavailable: "هذا الدليل لم يعد متاحًا.", start_conversation: "بدء محادثة", leave_message: "ترك رسالة", search_help: "البحث عن مساعدة", submit_request: "إرسال طلب مفصل", share_feedback: "مشاركة ملاحظات",
    sending: "جار الإرسال…", sent: "تم الإرسال", failed_retry: "فشل — اضغط لإعادة المحاولة", agent_typing: "يكتب أحد الوكلاء", search_articles: "البحث في المقالات",
    articles_unavailable: "المقالات غير متاحة الآن. حاول مرة أخرى بعد قليل.", nothing_matched: "لا توجد نتائج. جرّب كلمات أقل أو ابدأ محادثة.", load_more_articles: "تحميل المزيد من المقالات", loading: "جار التحميل…",
    feedback_unavailable: "الملاحظات غير متاحة.", load_more_feedback_boards: "تحميل المزيد من اللوحات", load_more_feedback: "تحميل المزيد من الملاحظات", no_public_feedback: "لا توجد لوحات ملاحظات عامة", no_feedback: "لا توجد ملاحظات هنا بعد",
    improve: "ما الذي ينبغي تحسينه؟", feedback_why: "أخبرنا لماذا يهم هذا…", submit_feedback: "إرسال الملاحظات", voted: "تم التصويت", vote: "تصويت · {count}", following: "متابَع", follow: "متابعة", identify_to_vote: "عرّف بنفسك قبل التصويت.", identify_to_follow: "عرّف بنفسك قبل متابعة الملاحظات.",
    request_forms_unavailable: "النماذج غير متاحة.", load_more_forms: "تحميل المزيد من النماذج", request_received: "تم استلام الطلب", request_sent: "تم إرسال طلبك إلى فريق الدعم.", no_request_forms: "لا توجد نماذج طلبات متاحة",
    start_conversation_instead: "ابدأ محادثة وسيساعدك أحد الأشخاص.", choose: "اختر…", uploading: "جار الرفع…", file_ready: "الملف جاهز", send_request: "إرسال الطلب", attachment_sent_error: "تم إرسال رسالتك، لكن تعذر رفع مرفق.", message: "رسالة", attach_file: "إرفاق ملف", powered_by: "مدعوم من Hubchat", you: "أنت", attachment_upload_error: "تعذر رفع الملف.", could_not_load_feedback: "تعذر تحميل الملاحظات.",
  },
};

function localeFor(value: string | undefined) {
  const base = (value || "en").trim().toLowerCase().replaceAll("_", "-").split("-", 1)[0] || "en";
  return messages[base] ? base : "en";
}

export type WidgetText = (key: string, fallback: string, values?: Values) => string;

export function widgetText(language: string | undefined, key: string, fallback: string, values?: Values) {
  let value = messages[localeFor(language)]?.[key] ?? messages.en?.[key] ?? fallback;
  for (const [name, replacement] of Object.entries(values ?? {})) value = value.replaceAll(`{${name}}`, String(replacement));
  return value;
}

const Context = createContext<WidgetText>((key, fallback, values) => widgetText("en", key, fallback, values));

export function WidgetLocaleProvider({ language, children }: { language?: string; children: ReactNode }) {
  const translate = useMemo<WidgetText>(() => (key, fallback, values) => widgetText(language, key, fallback, values), [language]);
  return <Context.Provider value={translate}>{children}</Context.Provider>;
}

export function useWidgetText() {
  return useContext(Context);
}
