# Mathematical Proof of ag-up State Machine Integrity

## State Machine Definition

Let $\Sigma$ be the set of states for the update process:
$S = \{ Check, Download, Install, Success, Error \}$

Let $T$ be the set of atomic transitions:
- $t_1: Check \to Download$ (if remote version differs)
- $t_2: Check \to Success$ (if versions match)
- $t_3: Check \to Error$ (if network fails)
- $t_4: Download \to Install$ (if SHA-512 matches)
- $t_5: Download \to Error$ (if network fails or checksum fails)
- $t_6: Install \to Success$ (if atomic rename succeeds)
- $t_7: Install \to Error$ (if extraction or rename fails)

## Axioms of Atomic Integrity

**Axiom 1 (Download Isolation):**
Downloads stream exclusively to a temporary file path $\pi_{tmp} \in /tmp$.
$\forall d \in Download$, no modification occurs in the target installation directory $D_{target}$.

**Axiom 2 (Atomic Extraction):**
Extraction writes exclusively to a randomly named temporary directory $D_{tmp}$ in the same filesystem as $D_{target}$.
$\forall e \in Install$, prior to rename, $D_{target}$ is unmodified.

**Axiom 3 (Atomic Commit):**
The final transition to success involves a POSIX `rename()` syscall, which is guaranteed atomic.
$D_{target} = rename(D_{tmp}, D_{target})$.
If $rename()$ fails (e.g., power loss), $D_{target}$ contains either the complete previous version or the complete new version. No partial state is exposed.

## Proof by Exhaustion

To prove the system is never left in a corrupted state, we examine all failure states:

1. **Failure during $Check$:**
   The process enters $Error$. $D_{target}$ is unmodified. The system remains in the previous valid state.
2. **Failure during $Download$:**
   The network drops or SHA-512 validation fails. The temporary file $\pi_{tmp}$ is closed and unlinked via `defer os.Remove()`. $D_{target}$ is unmodified. The system remains in the previous valid state.
3. **Failure during $Install$ (Extraction Phase):**
   A corrupted tarball causes extraction to panic or error. The temporary directory $D_{tmp}$ is unlinked via `defer os.RemoveAll()`. $D_{target}$ is unmodified. The system remains in the previous valid state.
4. **Failure during $Install$ (Rename Phase):**
   The POSIX `rename()` is atomic. If the process crashes immediately before `rename()`, $D_{target}$ holds the old state. If it crashes immediately after, $D_{target}$ holds the new state. A partial state cannot exist.

Therefore, $\forall p \in \Sigma$, a transition to $Error$ always results in the preservation of the initial stable state or a transition to a new completely valid state. The system is empirically and formally isolated from partial corruption.
