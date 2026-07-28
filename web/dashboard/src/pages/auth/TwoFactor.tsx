import { api, ApiError, Button, Callout, clearQueryCache, Field, Input } from "@hubchat/shared";
import { useMutation } from "@hubchat/shared";
import { ShieldCheck } from "lucide-react";
import { useRef, useState } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";

const LENGTH = 6;

type LocationState = { challenge?: string; next?: string | null };

export default function TwoFactor() {
  const navigate = useNavigate();
  const location = useLocation();
  const state = (location.state as LocationState | null) ?? {};

  const [digits, setDigits] = useState<string[]>(Array(LENGTH).fill(""));
  const [recoveryCode, setRecoveryCode] = useState("");
  const [useRecovery, setUseRecovery] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputs = useRef<(HTMLInputElement | null)[]>([]);

  const verify = useMutation<{ challenge: string; code: string }, unknown>(
    (body) => api.post("/auth/totp/challenge", body),
    {
      onSuccess: () => {
        clearQueryCache();
        navigate(state.next ?? "/overview", { replace: true });
      },
      onError: (caught) => {
        setError(
          caught instanceof ApiError
            ? caught.message
            : "Could not reach the server. Check your connection and try again.",
        );
      },
    },
  );

  // Arriving here without a challenge means someone opened the URL directly
  // rather than being sent here by a real sign-in — there is nothing to
  // verify against, so send them back to start over.
  if (!state.challenge) {
    return <Navigate to="/login" replace />;
  }

  const setDigit = (index: number, value: string) => {
    const cleaned = value.replace(/\D/g, "");
    if (!cleaned) return;

    setDigits((current) => {
      const next = [...current];
      cleaned.split("").forEach((char, offset) => {
        if (index + offset < LENGTH) next[index + offset] = char;
      });
      return next;
    });

    const target = Math.min(index + cleaned.length, LENGTH - 1);
    inputs.current[target]?.focus();
  };

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    const code = useRecovery ? recoveryCode : digits.join("");
    void verify.mutate({ challenge: state.challenge!, code }).catch(() => {});
  };

  return (
    <>
      <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
        <ShieldCheck aria-hidden="true" className="size-5 text-accent-text" />
      </div>

      <h1 className="text-xl font-semibold tracking-tight text-fg">Two-factor authentication</h1>
      <p className="mt-2 text-sm text-fg-muted">
        {useRecovery
          ? "Enter one of the recovery codes you saved when enabling two-factor."
          : "Enter the six-digit code from your authenticator app."}
      </p>

      {error && (
        <Callout tone="danger" className="mt-4">
          {error}
        </Callout>
      )}

      <form onSubmit={submit} className="mt-6 flex flex-col gap-4">
        {useRecovery ? (
          <Field label="Recovery code" htmlFor="recovery">
            <Input
              id="recovery"
              mono
              inputSize="lg"
              placeholder="xxxxx-xxxxx"
              autoFocus
              value={recoveryCode}
              onChange={(event) => setRecoveryCode(event.target.value)}
            />
          </Field>
        ) : (
          <div>
            <fieldset>
              <legend className="sr-only">Six-digit verification code</legend>
              <div className="flex gap-2">
                {digits.map((digit, index) => (
                  <input
                    key={index}
                    ref={(element) => {
                      inputs.current[index] = element;
                    }}
                    value={digit}
                    onChange={(event) => setDigit(index, event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Backspace" && !digits[index]) {
                        inputs.current[index - 1]?.focus();
                      }
                    }}
                    inputMode="numeric"
                    autoComplete={index === 0 ? "one-time-code" : "off"}
                    maxLength={LENGTH}
                    aria-label={`Digit ${index + 1}`}
                    autoFocus={index === 0}
                    className="h-12 w-full rounded-md border border-line bg-inset text-center font-mono text-lg text-fg outline-none transition-colors focus:border-accent focus:bg-surface focus:shadow-[0_0_0_3px_var(--hc-accent-subtle)]"
                  />
                ))}
              </div>
            </fieldset>
          </div>
        )}

        <Button type="submit" variant="primary" size="lg" fullWidth loading={verify.isPending}>
          Verify
        </Button>
      </form>

      <div className="mt-6 flex flex-col gap-2 text-center text-xs">
        <button
          type="button"
          onClick={() => setUseRecovery((current) => !current)}
          className="text-accent-text hover:underline"
        >
          {useRecovery ? "Use your authenticator app instead" : "Use a recovery code instead"}
        </button>
        <Link to="/login" className="text-fg-muted hover:text-fg">
          Back to sign in
        </Link>
      </div>
    </>
  );
}
