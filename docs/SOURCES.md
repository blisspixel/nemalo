# Source directory and integration research

Research date: 2026-10-08. Open Library/Internet Archive metadata search and Archive
item/file evaluation and selected EPUB/PDF/MP3 intake are implemented. Other integrations below remain
planned. See [decision 0005](decisions/0005-provider-search-and-evaluation.md) for
implemented contracts and limits. The [acquisition guide](ACQUISITION.md)
distinguishes metadata from actual transfers. This directory
covers books, scholarly literature, textbooks, audio, children's reading, and
multilingual collections. It is a maintained shortlist, not a claim to list every
repository worldwide or a promise to implement every source.
The [validation harness](VALIDATION.md) uses explicitly selected Gutenberg-mirror,
LibriVox/Archive, and arXiv assets without implementing their production adapters.

## How to read this directory

The core table records primary documentation reviewed in this research. It is not
an end-to-end integration test. Candidate tables preserve the broader requested
directory with suggested evaluation roles; their APIs, availability, formats,
current terms, and automation permissions have not all been verified. Homepage
links are starting points, not technical-contract citations.

Separate metadata discovery, local acquisition, web reading, streaming, lending,
and redistribution. Store rights and available formats per edition/recording/asset.
A site's metadata license does not license its PDFs. Public-domain declarations
can be jurisdiction-specific. Unknown rights remain visible and unresolved.

P0 means the first end-to-end discovery/evaluation/acquisition cohort, developed
alongside local intake and sharing its assessment/publication pipeline.
P1 broadens books and adds dedicated audio/paper cohorts. P2 is later evaluation.
These are integration priorities, not a website quality or trust ranking. Discovery
and acquisition are core product capabilities, not later optional add-ons. See
the [roadmap](../ROADMAP.md) for dependencies and release sequencing.

## Core integrations: documentation reviewed

| Source | Priority and role | Interface and constraints supported by primary sources |
| --- | --- | --- |
| [Project Gutenberg](https://www.gutenberg.org/) | P0 classics and multilingual ebooks | Use machine-readable catalogs and permitted acquisition/harvest or mirror routes. Do not automate ordinary website search pages. See [robot policy](https://www.gutenberg.org/policy/robot_access.html) and [catalog guidance](https://www.gutenberg.org/ebooks/offline_catalogs.html). Retain [US-based rights statements](https://www.gutenberg.org/policy/permission.html). |
| [Internet Archive](https://archive.org/) | P0 selected books; P1 audio | Search and item/file metadata through [developer interfaces](https://archive.org/developers/). Resolve actual public files; account for private, restricted, lending, and missing assets. [Download guidance](https://archivesupport.zendesk.com/hc/en-us/articles/360016398872-Downloading-A-Basic-Guide) does not imply all items are downloadable or reusable. |
| [Open Library](https://openlibrary.org/) | P0 bibliographic discovery | [APIs](https://openlibrary.org/developers/api) for human lookups, not catalog harvesting. Default 1 request/second, properly identified clients 3/second. Bulk work uses monthly dumps. [Search documentation](https://openlibrary.org/dev/docs/api/search) separates works and matching editions; availability must be resolved for the selected edition. |
| [LibriVox](https://librivox.org/) | P1 primary audiobook catalog | [API](https://librivox.org/api/info): default limit 50, maximum 500 since the September 2026 update; paginate with `offset`, use `since` for newly cataloged projects, space requests. Extended records include sections and Archive references. Recording rights use a [US-based public-domain declaration](https://librivox.org/pages/public-domain/). |
| [Standard Ebooks](https://standardebooks.org/) | P1 curated classic editions | [Feed policy](https://standardebooks.org/feeds): public new-release RSS/Atom; other feeds require eligible access. OPDS can serve JSON with the documented Accept header. Do not assume unrestricted whole-catalog OPDS access or replace denied access with scraping. |
| [OpenStax](https://openstax.org/) | P1 textbooks | Official [format guidance](https://help.openstax.org/s/article/Why-don-t-you-have-epub-versions-of-your-books) says EPUB downloads are discontinued; web/PDF/DOCX/print are listed. Evaluate actual book assets and licenses, not an EPUB conversion promise. A stable acquisition contract needs implementation-time verification. |
| [LibreTexts Commons](https://commons.libretexts.org/) | P1 open textbook discovery | Reviewed the official [Conductor implementation](https://github.com/LibreTexts/conductor), including book/search models. Commons search exposes book licenses and offered links, commonly web/PDF. Do not infer EPUB or language support from other providers. This is a website implementation interface, not a promised stable public API. |
| [DOAB](https://www.doabooks.org/) | P1 scholarly-book discovery | Official [metadata documentation](https://www.doabooks.org/en/doab/metadata-harvesting-and-content-dissemination) identifies harvesting/search facilities. Search indexing exposed REST examples; direct page retrieval returned 403 in this research. Reconfirm endpoint, schema, and use terms before building an adapter. Do not conflate DOAB records with hosted files. |
| [OAPEN](https://www.oapen.org/) | P1 scholarly-book host/resolver | [Metadata exports and OAI-PMH](https://www.oapen.org/article/metadata) plus documented [REST search and bitstreams](https://www.oapen.org/article/8185269-search-using-a-rest-api). CC0 metadata does not replace each book's license. Handle books versus chapters and records without downloadable assets. |
| [OpenAlex](https://openalex.org/) | P1 broad paper discovery | Current [authentication guidance](https://help.openalex.org/api/authentication/) permits limited keyless basic use; a free key raises the budget. Usage is metered and heavier use can cost money. Earlier [February guidance](https://blog.openalex.org/openalex-api-new-features-and-usage-based-pricing/) was stricter about keys. Use a configured key/budget for sustained integration and recheck policy. |
| [Unpaywall](https://data.unpaywall.org/) | P1 DOI-to-open-copy resolution | [API reference](https://data.unpaywall.org/products/api): `/v2/:doi` remains; requests include a contact email. `/v2/search` retired 2026-09-18 and returns 410. Use OpenAlex for search, Unpaywall for DOI resolution. |
| [arXiv](https://arxiv.org/) | P1 versioned preprints | [API terms](https://info.arxiv.org/help/api/tou.html): legacy APIs use one connection and at most one request every three seconds. Metadata and paper licenses differ. Preserve arXiv version IDs and licensing; link preprints and later publications without treating them as identical files. |
| [PubMed Central](https://pmc.ncbi.nlm.nih.gov/) | P1 biomedical full text | [OAI service](https://pmc.ncbi.nlm.nih.gov/tools/oai/) and [OA subset](https://pmc.ncbi.nlm.nih.gov/tools/openftlist/) define supported retrieval and reuse boundaries. PMC presence alone is not reuse permission; use approved automated channels and per-article licensing. PDF resolution may differ from full-text XML retrieval. |
| [Europe PMC](https://europepmc.org/) | P1 biomedical search and OA resolution | [REST service](https://europepmc.org/RestfulWebService) exposes search and metadata; `fullTextXML` is for the OA subset. Preserve source identifiers and version/type evidence. A search hit does not guarantee an unrestricted PDF. |

`since` on LibriVox selects newly cataloged projects, not necessarily every edit
to an older record. Periodic reconciliation is needed if Nemalo keeps an offline
catalog. An Archive-hosted ZIP and the LibriVox recording are two provenance links
to one recording, not automatically two audiobooks.

Gutenberg's robot policy is more restrictive than the mere existence of an OPDS
web response suggests. The Go design must use permitted catalog/acquisition
channels, even where a prototype successfully called another endpoint.

arXiv's [OAI documentation](https://info.arxiv.org/help/oa/index.html) specifies
`https://oaipmh.arxiv.org/oai`, replacing the old `export.arxiv.org/oai2` base in
March 2025. Its `arXiv` metadata format exposes license information for the latest
record; `arXivRaw` also exposes history. Do not assign a latest-record license to
older file versions without evidence that it applies.

## Corrections and implementation caveats

- OpenStax is useful without EPUB. PDF collection support must stand on its own.
- Standard Ebooks' public new-release feed and restricted catalog feeds have
  different access rules. Free individual books do not imply open feed access.
- OpenAlex's current help and older announcements differ. Follow current endpoint
  guidance, observe actual budget headers, and stop at the user's configured limit.
  Do not assume unlimited free searching or silently buy credits.
- Open Library remains a discovery service. Do not pick an arbitrary Archive ID
  from work-level search and assume it matches the requested language/edition.
- LibriVox's 500-record maximum and Unpaywall's retired search endpoint are confirmed
  in their current official documentation. No live download test was done here.
- DOAB/OAPEN are separate capabilities. Metadata access and hosted full text need
  separate resolution and licensing checks.
- Free web reading, a public PDF URL, and open licensing are different properties.
  Peer review, OCR quality, and audiobook completeness are separate evidence again.

## Candidate literature and multilingual libraries

These are P2 evaluation candidates unless elevated by a concrete use case. Roles
below describe what to investigate, not verified automation contracts.

| Candidate | Evaluate for |
| --- | --- |
| [Google Books](https://books.google.com/) | Bibliographic lookup and eligible public-domain downloads |
| [Wikisource](https://wikisource.org/) | Multilingual literature and permitted exports |
| [HathiTrust](https://www.hathitrust.org/) | Research-library discovery; account/institution-dependent access |
| [Global Grey](https://www.globalgreyebooks.com/) | Classic editions and offered formats |
| [Faded Page](https://www.fadedpage.com/) | Proofread editions under Canadian rights declarations |
| [Planet eBook](https://www.planetebook.com/) | Curated classics and actual download formats |
| [ManyBooks](https://manybooks.net/) | Classics and contemporary free-title discovery |
| [Project Gutenberg Australia](https://gutenberg.net.au/) | Australian public-domain literature |
| [Project Gutenberg Canada](https://gutenberg.ca/) | Canadian public-domain literature |
| [Project Runeberg](https://runeberg.org/) | Nordic literature and digitizations |
| [Gallica](https://gallica.bnf.fr/) | French national-library editions, scans, and exports |
| [Europeana](https://www.europeana.eu/) | European discovery and links to owning repositories |
| [DPLA](https://dp.la/) | US cultural-heritage discovery |
| [Library of Congress](https://www.loc.gov/) | Historical books, reports, and item-level assets |
| [Biodiversity Heritage Library](https://www.biodiversitylibrary.org/) | Historical natural-history literature |
| [Biblioteca Virtual Miguel de Cervantes](https://www.cervantesvirtual.com/) | Spanish and Latin American reading collections |
| [Aozora Bunko](https://www.aozora.gr.jp/) | Japanese literary texts and their source encodings |
| [Project Madurai](https://www.projectmadurai.org/) | Tamil texts and downloadable editions |
| [Chinese Text Project](https://ctext.org/) | Classical Chinese discovery and permitted structured access |
| [Project Ben-Yehuda](https://benyehuda.org/) | Hebrew literature |
| [Internet Sacred Text Archive](https://sacred-texts.com/) | Historical religion, mythology, and folklore |

Country-specific public-domain projects require jurisdiction-aware presentation.
Do not derive worldwide copyright status from a source's country or the age of
the original work. Translations, editorial contributions, and recordings can differ.

## Candidate textbooks and scholarly books

| Candidate | Evaluate for |
| --- | --- |
| [Open Textbook Library](https://open.umn.edu/opentextbooks/) | Reviewed textbook discovery and licensed asset links |
| [BCcampus Open Collection](https://collection.bccampus.ca/) | Open textbooks and export formats |
| [eCampusOntario Open Library](https://openlibrary.ecampusontario.ca/) | Higher-education resources |
| [Pressbooks Directory](https://pressbooks.directory/) | Cross-site book discovery; per-book exports and rights |
| [OER Commons](https://oercommons.org/) | Educational discovery and external destinations |
| [MERLOT](https://www.merlot.org/) | Reviewed educational-resource metadata |
| [Saylor Academy](https://www.saylor.org/) | Course-associated texts and materials |
| [Wikibooks](https://www.wikibooks.org/) | Community textbooks and permitted exports |
| [MIT OpenCourseWare](https://ocw.mit.edu/) | Course notes, texts, and reports |
| [Open Book Publishers](https://www.openbookpublishers.com/) | Scholarly editions and per-format licenses |
| [Open Humanities Press](https://www.openhumanitiespress.org/) | Humanities books |
| [punctum books](https://punctumbooks.com/) | Humanities and interdisciplinary books |
| [UCL Press](https://uclpress.co.uk/) | Open university-press editions |
| [MIT Press Direct](https://direct.mit.edu/) | Open-access subset of academic publishing |
| [Cambridge Open Access](https://www.cambridge.org/core/what-we-publish/open-access) | OA book/article subsets |
| [Oxford Academic](https://academic.oup.com/) | OA book/article subsets |
| [Springer Nature Link](https://link.springer.com/) | OA books, chapters, and articles |
| [JSTOR Open Content](https://about.jstor.org/oa-and-free/) | OA scholarly and primary-source subsets |
| [IntechOpen](https://www.intechopen.com/) | Scientific books and chapters |
| [MDPI Books](https://www.mdpi.com/books) | Scientific books and edited volumes |
| [SciELO Books](https://books.scielo.org/) | Latin American scholarly books |
| [CLACSO](https://biblioteca-repositorio.clacso.edu.ar/) | Social-science books and reports |
| [National Academies Press](https://nap.nationalacademies.org/) | Scientific reports and eligible downloads |
| [NCBI Bookshelf](https://www.ncbi.nlm.nih.gov/books/) | Biomedical reference texts; per-title access terms |
| [Unglue.it](https://unglue.it/) | Licensed modern ebooks and OPDS discovery |

## Modern books and acquisition destinations

Default to outbound discovery until a permitted, supported acquisition interface
and item-level rights are verified. Zero price does not grant redistribution rights
or justify DRM removal.

| Candidate | Evaluate for |
| --- | --- |
| [Baen Free Library](https://www.baen.com/allbooks/category/index/id/2012) | Publisher-provided free editions |
| [Smashwords](https://www.smashwords.com/) | Free independent-author titles |
| [Kobo](https://www.kobo.com/) | Free store offerings and account requirements |
| [Google Play Books](https://play.google.com/store/books) | Free offerings and acquisition terms |
| [Apple Books](https://www.apple.com/apple-books/) | External store discovery |
| [Kindle Store](https://www.amazon.com/Kindle-eBooks/) | External store discovery |
| [Leanpub](https://leanpub.com/) | Author-configured free or minimum-price editions |

Unglue.it is listed with scholarly/open books above rather than duplicated here.

## Scientific search, resolvers, and directories

OpenAlex and Unpaywall are in the researched core. Broaden only after the initial
paper workflow works with real identifiers, versions, citations, and PDF policies.

| Candidate | Evaluate for |
| --- | --- |
| [CORE](https://core.ac.uk/) | OA aggregation; [API/FAQ](https://core.ac.uk/faq) documents rate and eligibility distinctions. Reconfirm the applicable key/license tier before integration. |
| [DOAJ](https://doaj.org/) | Journal/article discovery and license metadata |
| [Semantic Scholar](https://www.semanticscholar.org/) | Scholarly metadata and public full-text locations |
| [BASE](https://www.base-search.net/) | Repository discovery; verify API access eligibility |
| [OpenAIRE Explore](https://explore.openaire.eu/) | Research-output discovery |
| [Google Scholar](https://scholar.google.com/) | External human search; no assumed automation API |
| [OpenDOAR](https://v2.sherpa.ac.uk/opendoar/) | Repository directory and additional source selection |
| [PubMed](https://pubmed.ncbi.nlm.nih.gov/) | Biomedical references and full-text links, distinct from PMC |
| [Crossref](https://www.crossref.org/) | DOI metadata and citation enrichment, not a universal full-text host |

## Scientific repositories and publishers

arXiv, PMC, and Europe PMC are in the core table. Treat repository or publisher
names as discovery context, not automatic proof of licensing or peer review.

| Candidate | Evaluate for |
| --- | --- |
| [bioRxiv](https://www.biorxiv.org/) | Biology preprints and versions |
| [medRxiv](https://www.medrxiv.org/) | Medical preprints and versions |
| [ChemRxiv](https://chemrxiv.org/) | Chemistry preprints |
| [PsyArXiv](https://osf.io/preprints/psyarxiv) | Psychology preprints |
| [SocArXiv](https://osf.io/preprints/socarxiv) | Social-science preprints |
| [EarthArXiv](https://eartharxiv.org/) | Earth-science preprints |
| [SSRN](https://www.ssrn.com/) | Working papers and access restrictions |
| [HAL](https://hal.science/) | Multidisciplinary deposited research |
| [Zenodo](https://zenodo.org/) | Versioned research records and selected document assets |
| [Figshare](https://figshare.com/) | Research-output files and per-record licensing |
| [OSF Preprints](https://osf.io/preprints/) | Federated preprint discovery |
| [PLOS](https://plos.org/) | Open journal articles |
| [eLife](https://elifesciences.org/) | Research articles and explicit review/version status |
| [BioMed Central](https://www.biomedcentral.com/) | Biomedical journal articles |
| [Frontiers](https://www.frontiersin.org/) | Open journal articles |
| [MDPI Journals](https://www.mdpi.com/) | Open journal articles |
| [PeerJ](https://peerj.com/) | Open research publications |
| [SciELO](https://scielo.org/) | Multilingual journal literature |
| [Redalyc](https://www.redalyc.org/) | Latin American scholarly literature |
| [Open Research Europe](https://open-research-europe.ec.europa.eu/) | Research and versioned review information |
| [Wellcome Open Research](https://wellcomeopenresearch.org/) | Health research and review information |
| [CERN Document Server](https://cds.cern.ch/) | Physics documents and reports |
| [INSPIRE](https://inspirehep.net/) | High-energy-physics discovery and asset links |
| [NASA NTRS](https://ntrs.nasa.gov/) | Aerospace reports and publications |
| [OSTI](https://www.osti.gov/) | Energy research and technical reports |
| [ERIC](https://eric.ed.gov/) | Education research; separate records from free full text |
| [IDEAS/RePEc](https://ideas.repec.org/) | Economics discovery and repository links |

Zenodo, Figshare, and OSF can include datasets or executable artifacts. Nemalo should
select supported document assets, not import an entire research record blindly.

## Audiobook and spoken-word candidates

LibriVox is the first audio catalog. Resolve permitted Archive-hosted recordings
through the same asset pipeline and preserve recording/track identity across sites.

| Candidate | Evaluate for |
| --- | --- |
| [Internet Archive Audio](https://archive.org/details/audio) | Public recording assets with item/file rights and restrictions |
| [Lit2Go](https://etc.usf.edu/lit2go/) | Educational texts and associated recordings |
| [Loyal Books](https://www.loyalbooks.com/) | Audio discovery; cross-source recording deduplication |
| [Digitalbook.io](https://www.digitalbook.io/) | Ebook/audio discovery and original-source links |
| [Storynory](https://www.storynory.com/) | Children's listening and explicitly permitted downloads |
| [Open Culture](https://www.openculture.com/freeaudiobooks) | Curated outbound listening links |
| [Project Gutenberg Audio](https://www.gutenberg.org/) | Catalog entries with actual audio assets |
| [BBC Sounds](https://www.bbc.co.uk/sounds) | External listening; regional/account/availability constraints |

Do not infer recording rights from text rights or a streaming player. Free listening
destinations remain links unless automated acquisition permission is established.

## Children's and multilingual literacy candidates

| Candidate | Evaluate for |
| --- | --- |
| [StoryWeaver](https://storyweaver.org.in/) | Multilingual stories; text, art, and audio licensing |
| [African Storybook](https://www.africanstorybook.org/) | African-language stories and editions |
| [Book Dash](https://bookdash.org/) | Illustrated children's books and attribution requirements |
| [Let's Read](https://www.letsreadasia.org/) | Asian-language reading collections |
| [Global Digital Library](https://digitallibrary.io/) | Early-literacy multilingual discovery |
| [Unite for Literacy](https://www.uniteforliteracy.com/) | Reading and narration destinations |
| [Free Kids Books](https://freekidsbooks.org/) | Children's books and per-item terms |
| [Bloom Library](https://bloomlibrary.org/) | Community-language publications and formats |
| [CK-12](https://www.ck12.org/) | School-level educational resources |

Record translation and illustration attribution separately where required. Language
support should reflect actual edition metadata, not just the interface language.

## Library borrowing destinations

Default role: outbound borrowing links, not unrestricted local acquisitions. No
borrowed-file decryption, account scraping, or lending-limit bypass is planned.

| Candidate | Evaluate for |
| --- | --- |
| [Libby / OverDrive](https://www.overdrive.com/apps/libby) | Participating-library borrowing |
| [Hoopla](https://www.hoopladigital.com/) | Participating-library borrowing |
| [cloudLibrary](https://www.yourcloudlibrary.com/) | Library borrowing destinations |
| [BorrowBox](https://www.borrowbox.com/) | Library borrowing destinations |
| [Open Library borrowing](https://openlibrary.org/) | Edition-specific borrowing links and current availability |

## Technical and institutional candidates

| Candidate | Evaluate for |
| --- | --- |
| [Free Programming Books](https://github.com/EbookFoundation/free-programming-books) | Multilingual discovery directory; evaluate each destination independently |
| [dBooks](https://www.dbooks.org/) | Technical-book discovery and original license evidence |
| [FreeComputerBooks](https://freecomputerbooks.com/) | Computing/math discovery links |
| [Open Logic Project](https://openlogicproject.org/) | Logic texts and source/publication relationships |
| [WHO IRIS](https://iris.who.int/) | Public-health documents |
| [World Bank Open Knowledge Repository](https://openknowledge.worldbank.org/) | Development research and reports |
| [UNESCO Digital Library](https://unesdoc.unesco.org/) | Multilingual reports and publications |
| [IMF eLibrary](https://www.elibrary.imf.org/) | Economics reports and eligible publications |
| [USGS Publications Warehouse](https://pubs.usgs.gov/) | Government reports; distinguish external journal records |
| [EU Publications Office](https://op.europa.eu/) | European technical and policy publications |
| [NDLTD](https://ndltd.org/) | Thesis/dissertation repository discovery |

## Adapter acceptance and maintenance

Before promoting any candidate to a supported adapter:

1. Verify an official API/feed/export contract, automated-use policy, authentication,
   quotas, billing, pagination, supported filters, and change announcements.
2. Document supported actions and formats, item-level rights fields, stable IDs,
   and canonical landing pages. Mark web-only and borrowing results accurately.
3. Build offline synthetic contract fixtures for success, missing fields, HTML
   errors disguised as 200, throttling, schema drift, and revoked assets.
4. Route resolved files through staging, checks, provenance, and verified publication.
   Enforce URL/redirect boundaries and provider budgets in shared services.
5. Run a small permitted live search/lookup/acquisition check when credentials and
   tools are available. Record provider, adapter, contract date, and actual result.
6. Expose health/failure state, cache age, and policy changes. Reverify before releases
   and after drift; disable unsafe acquisition without hiding metadata discovery.

Provider errors must not cause an unannounced fallback to another source or edition.
Users choose what enters their collection. Saved searches and small reviewed
collection manifests can help build classics by language without unbounded mirrors.

Acquisition ranking should explain format, language, edition, rights, and available
quality evidence. Download popularity, publisher reputation, and deterministic
keyword scores do not prove completeness, relevance, safety, or peer review.

## Reviewed and declined

Library Genesis was reviewed on 2026-10-09 as a possible optional search source.
It is not a Nemalo provider.

It is a shadow library. The [Wikipedia article](https://en.wikipedia.org/wiki/Library_Genesis)
describes file-sharing access to works that are otherwise paywalled, and records
publisher litigation and domain seizures. A search hit from that catalog is
file-location evidence, not a rights declaration.
[Decision 0005](decisions/0005-provider-search-and-evaluation.md) requires one
fixed HTTPS host and no configurable endpoints. Public descriptions list several
domains, and those domains change under blocking and seizure. The JSON interface
at [libgen.li/json.php](https://libgen.li/json.php), reviewed the same day, returns
object records by identifier, DOI, hash, or time range. It does not define title
search, an automated-use quota, or a stability promise. Community clients scrape
HTML and then request those records. That fails the fixed-host and non-scraping
adapter rules above.

Open Library remains the bibliographic search provider. Textbook and scholarly
coverage stays with the sources in this directory that publish access terms,
including OpenStax, LibreTexts, DOAB, OAPEN, OpenAlex, and Unpaywall. Revisit
Library Genesis only if one operator publishes a fixed-host metadata search
contract, an automated-use policy, and item-level rights that can be stored as
declarations. File retrieval from it is out of scope.
