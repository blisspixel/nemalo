# Cited evidence: selected reading review

Date: 2026-10-23. This records actual responses over the implemented
[evidence contract](CONTENT-ACCESS.md#evidence-and-citation-export), followed by
a same-session review against the returned passages. It supports the five selected
[milestone 9 cases](../ROADMAP.md#milestone-9-cited-evidence-and-agent-reading).
It is not independent evaluation, whole-work reading or a general reliability
claim. Native installed-tool checks are Linux-specific. Full local/native/hosted
verification is a separate acceptance gate recorded on
[issue 39](https://github.com/blisspixel/nemalo/issues/39).

## Sources and exact ranges

The permitted cached sources were Gutenberg 1342 (Pride and Prejudice), 3420
(A Vindication of the Rights of Woman), 3600 (the Cotton/Hazlitt Montaigne edition)
and arXiv 1706.03762v7. No provider request ran. The earlier paper version retained
its own uncertain license rather than borrowing version 7's declaration.
Selected metadata and citation exports retain unknown authorship/date/type fields
when those are absent from the journals; recognizing a name in retrieved text does
not silently fill the CSL export.

Each identifier below is the full SHA-256 value, without the `sha256:` prefix.
The extractor and representation identify the actual returned text rules.

| Source | Asset SHA-256 | Extractor | Representation SHA-256 |
| --- | --- | --- | --- |
| Gutenberg 1342 | `e59e587472723023b21bf380b8327858e7540b25e57affa3a68935b291e370dc` | `nemalo.epub.nav/1` | `ad354f926398421ed05721b5b260b3eebe897c6fb3652c37092c00ed85f53575` |
| Gutenberg 1342 | `e59e587472723023b21bf380b8327858e7540b25e57affa3a68935b291e370dc` | `nemalo.epub.text/2` | `9339eac848aec364684296b998814fa773a55d129991aa50dc9ab9a8355ad087` |
| Gutenberg 3420 | `6c11e12aa246d08d84480763a7a1d954f3488dd226e53bba15b14ab58b7e29e8` | `nemalo.epub.nav/1` | `55ec66232608ec00533fb7df4be828ddfaf7656588ddc0495e34277d7a35a324` |
| Gutenberg 3600 | `627526d9cfa73d7aa2351efe2e30cb3a1cbd7fd7ada98c5c4e2d9cddf851a834` | `nemalo.epub.nav/1` | `b3b70b1ef695c68775265112c47fd4320d98afa36344c91a356b0face1ef6451` |
| arXiv 1706.03762v7 | `bdfaa68d8984f0dc02beaca527b76f207d99b666d31d1da728ee0728182df697` | `nemalo.pdf.text/1` | `5e636d8bf58c83ada442bac3ea9d79aaf820508c25b3d87d372b654d172f7044` |

Ranges are zero-based, half-open UTF-8 bytes within the named spine unit or PDF
physical page. A navigation selector is a declared entry order, distinct from the
returned spine unit. Full request selections, exact quotations, metadata, coverage,
references and result hashes are retained in the local evidence index. Record
names in brackets in the responses refer to this table.

| Record | Representation | Returned unit/page | Byte range | Located gaps | Continuation |
| --- | --- | --- | --- | --- | --- |
| `pride-opening` | `epub-nav/1` | 1 | 27165..28000 | 0 | yes |
| `pride-opening-continued` | `epub-nav/1` | 2 | 0..3967 | 0 | no |
| `pride-early` | `epub-nav/1` | 2 | 8514..18370 | 0 | no |
| `pride-revision` | `epub-text/2` | 10 | 3100..6900 | 0 | yes |
| `vindication-reason` | `epub-nav/1` | 1 | 34026..51392 | 0 | no |
| `vindication-education` | `epub-nav/1` | 1 | 22517..34026 | 0 | no |
| `montaigne-education` | `epub-nav/1` | 10 | 24394..42394 | 0 | yes |
| `montaigne-method` | `epub-nav/1` | 10 | 42394..51218 | 0 | yes |
| `paper-abstract` | `pdf-text/1` | 0 | 0..2897 | 0 | yes |
| `paper-attention` | `pdf-text/1` | 2 | 0..1839 | 1 | yes |
| `paper-method` | `pdf-text/1` | 3 | 0..2560 | 3 | yes |
| `paper-cost` | `pdf-text/1` | 5 | 0..3579 | 0 | yes |
| `paper-training` | `pdf-text/1` | 6 | 0..3407 | 0 | yes |
| `paper-results` | `pdf-text/1` | 7 | 0..3288 | 0 | yes |
| `paper-variations` | `pdf-text/1` | 8 | 0..2990 | 0 | yes |

All 17 selected CLI results replayed exactly, including these passages plus
separate original MP3 and decoded MP4 access checks. Four actual stdio MCP
selections and their replays matched the CLI. Library inventories/hashes stayed
unchanged. Five actual citation arrays passed the pinned official CSL schema.
These support provenance and repeatability, not the semantic judgments below.

## Reviewed responses

## Does the early judgment of Darcy survive Elizabeth's later reflection?

The assembly first admires Darcy's appearance and reported wealth, then treats
his reserve as proof that he is disagreeable. His actual slight to Elizabeth is
also present, and her response gives the judgment a personal stake. The early
passage supports an account of injured pride and rapid social judgment, not a
claim that her dislike was invented without provocation. [pride-early]

Later, Elizabeth calls her own judgment blind, partial, prejudiced and absurd.
She reconsiders Wickham, recalls contrary observations of Darcy and
recognizes that flattery from one man and neglect from another shaped her
beliefs. She rereads the letter and revises her assessment of Jane's apparent
feelings and her family's conduct. The later passage challenges the adequacy of
her initial judgment without proving that Darcy's manners were never proud.
This is a change in her account of her own discernment, not a simple reversal
from one perfectly known character to another. [pride-revision]

Coverage: chapter III's selected declared range and a later 3800-byte passage
around the self-knowledge declaration were read. The intervening chapters,
the whole letter and the complete later chapter were not read for this answer.
These selected ranges report no located gaps. That absence does not establish
rendered or semantic completeness; no content has been inferred for uninspected
material. [pride-early; pride-revision]

## What are two defensible readings of the opening marriage claim?

One reading emphasizes economic and social appropriation. The opening makes a
wealthy unmarried man's supposed desire sound universal, but the next paragraph
admits that his feelings may be unknown and locates the conviction in the
surrounding families. Calling him their daughters' prospective property exposes
whose desire is being spoken for. Mrs. Bennet's subsequent urgency makes that
social interest concrete. [pride-opening; pride-opening-continued]

A second reading emphasizes the difficulty of knowing another person's mind.
The confident assertion is immediately set beside ignorance of the man's actual
views. Read alongside Elizabeth's later correction of her own confident
discernment, it can invite suspicion of judgments made from desire and vanity.
That is an interpretive connection between two cited locations. It does not prove
that every narrator statement is unreliable or that the whole novel has a single
epistemological message. [pride-opening; pride-revision]

These readings differ in emphasis: one asks whose social interests shape the
claim, the other asks what licenses its certainty. They can coexist. A flat
literal endorsement of the sentence's universality is poorly supported by the
immediately qualifying paragraph. [pride-opening]

Coverage: both returned parts of the opening declared chapter were inspected.
Its range crosses spine units 1 and 2; the first unit boundary was not the chapter
end. The continuation reaches range_end inside unit 2. The complete novel and
alternative critical traditions were not surveyed. [pride-opening;
pride-opening-continued]

## Does Attention Is All You Need show universally superior translation at lower cost?

The selected version supports a narrower experimental claim. Its Transformer
uses encoder/decoder stacks combining attention with position-wise feed-forward
layers, residual connections and normalization. The decoder also masks future
positions. Describing it as attention alone would hide these components.
[paper-attention; paper-method]

The training report uses WMT 2014 English-German and English-French data, with
about 4.5 million and 36 million sentence pairs respectively, specified token
representations and batches, and eight P100 GPUs. The reported big-model
English-German result is 28.4 BLEU; the evaluation also uses specified checkpoint
averaging, beam size and length penalty. Training-cost comparisons estimate
operations from time, GPU count and assumed sustained floating-point capacity.
These details limit the claim to the reported setups and comparisons, rather
than all tasks or all notions of efficiency. [paper-training; paper-results]

The self-attention discussion explicitly makes a complexity comparison conditional
on sequence length being smaller than representation dimensionality, and proposes
restricted attention for very long sequences as future work. Component variations
are evaluated on a development set under a different checkpoint-averaging setup;
more attention heads are not uniformly better. This supports qualified design
tradeoffs, not a universal performance guarantee. [paper-cost; paper-training;
paper-variations]

There is an unresolved numerical discrepancy in the retrieved version 7 page:
Table 2 shows 41.8 for the big model's English-French BLEU, while the adjacent
prose reports 41.0. Both must remain visible. The available text does not settle
which is intended, and the flattened table is not a rendered layout verification.
[paper-results]

Coverage: PDF physical pages 0, 2, 3, 5, 6, 7 and 8 were retrieved, with their
printed labels where available. This answer relies on the cited method, training,
cost and results pages. Figure/formula extraction gaps and unknown layout remain
explicit. The remaining pages, complete appendix, cited prior work, training code
and independent replications were not reviewed. The exact source is arXiv
1706.03762v7; no statement here silently substitutes version 1. [paper-attention;
paper-method; paper-cost; paper-training; paper-results; paper-variations]

## Do Montaigne and Wollstonecraft mean the same thing by independent judgment?

There is a useful limited agreement: both selected discussions resist education
that leaves a person dependent on an external appearance or authority.
Montaigne's tutor should prefer manners and judgment to accumulated learning;
the pupil must apply lessons in different circumstances, and the test is their
life rather than recitation. The continuation asks the learner to sift opinions,
choose when able and remain in doubt when unable. [montaigne-education;
montaigne-method]

Wollstonecraft's introduction attributes women's cultivated weakness and concern
with attractiveness to a false education that treats them as sexual ornaments
rather than human creatures with improvable reason. Chapter I grounds human
improvement in reason, virtue and knowledge, while warning that people use reason
to justify inherited prejudices and plausible conclusions from partial experience.
[vindication-education; vindication-reason]

The shared summary conceals different emphases and starting points. Montaigne's
selected proposal concerns a well-born male pupil and practical appropriation of
learning, including the legitimacy of unresolved doubt. Wollstonecraft's selected
argument makes claims about common human capacities and moral duties as a basis
for criticizing a social education of women. They are not simply skepticism
versus reason: Montaigne invokes shared truth and reason, and Wollstonecraft
acknowledges experience and the misuse of reasoning. [montaigne-education;
montaigne-method; vindication-education; vindication-reason]

Their historical assumptions also matter. Montaigne's opening discussion limits
the proper office of learning by social station. Wollstonecraft's introduction
retains claims about physical male superiority and the language of marriage even
while criticizing women's reduction to adornment. This is not evidence that
either selected text straightforwardly states a contemporary, unrestricted
equality position. [montaigne-education; vindication-education]

Coverage: selected portions of the declared education chapter, its continuation,
the introduction and chapter I were read. These editions, including Montaigne's
translation and editorial material, are distinct source bytes. Neither complete
work nor the original French was reviewed; the comparison is limited to these
passages. [montaigne-education; montaigne-method; vindication-education;
vindication-reason]

## How does a changed question change the reading?

For a question about the opening narrator's marriage claim, the relevant reading
begins with the declared opening chapter and follows its continuation across the
spine boundary. Those passages support the contrast between a universal-sounding
sentence and the families' interests. [pride-opening; pride-opening-continued]

Changing the question to whether Elizabeth's first judgment of Darcy is fair
requires different evidence: the assembly scene and the later self-correction,
located in a later unit and read with its surrounding context. The opening alone
does not answer that question. These additional retrieved ranges change the
answer's support and qualification; they are not a relabeling of the first
excerpt. [pride-early; pride-revision]

Neither set establishes the full novel's treatment of marriage, character or
knowledge. The exact record selections and continuations remain in the evidence
index so the difference can be checked rather than asserted as whole-work reading.


## Review and limitations

The review checked the actual responses against the retrieved source passages,
not just against references or a schema. Two initial presentation errors were
corrected before accepting this response set: a blanket sentence claimed that
the early Austen range reported gaps, although both cited ranges reported zero;
and a quotation reflowed a source newline. The correction states the actual gap
observation with its limits and uses explicit paraphrase. Original drafts remain
in the local audit record. Source bytes, ranges, digests and references were not
changed to make the responses fit.

The corrected cases meet their selected acceptance criteria:

- Early/later development cites both the assembly and later self-correction,
  preserving the real slight and the limits of Elizabeth's revision.
- Two defensible readings distinguish social appropriation from the authority
  of a confident knowledge claim without manufacturing mutually exclusive views.
- The paper explanation retains method, data, evaluation choices and limitations,
  including the unresolved 41.8/41.0 table/prose discrepancy in the selected version.
- The education comparison keeps definitions, historical assumptions, reason,
  application and doubt distinct rather than equating similar summaries.
- The changed question leads to different actual ranges, visible in the request
  records and locators, with unread material stated.

This is a bounded same-session review, not an independent assessment, original
French comparison, scientific replication, broad evaluation corpus or scoring
system. No model execution, generated interpretation store or reading state was
added to Nemalo. The application returns sources; the responses and review are
external evaluation artifacts.

The full native records remain outside the checkout at the task-owned local
validation run. SHA-256 artifact identities:

| Artifact | SHA-256 |
| --- | --- |
| Reviewed responses | `75969a4466965c11e33f7c3e4163688f5faf4b3783350ea7e5a672adc0fc119b` |
| Final local review | `2201248971e684d307a418d30e33eb27ff0b875b69c036ce658bdd04230b92ac` |
| Evidence index | `1b5b764031cea316771eaf2e21832e732e290bf3cf5645d47243fbe79af3f807` |

The index includes the exact source response hashes and replay references; it
is an audit artifact, not an executable command or permission grant.
