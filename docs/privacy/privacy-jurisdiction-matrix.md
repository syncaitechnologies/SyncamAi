# Privacy jurisdiction and primary-source matrix

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. **All sources below checked on 2026-09-08.** Dates in the version column describe the instrument or published source, not legal approval. A dated consolidated site may lag amendments; counsel must recheck official updates at deployment and incident time. Primary text prevails over summaries. This matrix deliberately gives no deployment clearance.

## Initial jurisdiction assessment

| Jurisdiction | Applicability determination | Required product response | Status |
|---|---|---|---|
| India | Digital processing territorial scope; employer/customer vs SyncCam purposes; staged provisions; IT/SPDI/CERT-In and sector rules | Itemized notices, lawful grounds, rights/grievance/nomination, processor/transfer and retention reconciliation | BLOCKED_APPROVAL |
| Canada federal | Commercial flows/FWUB; employment only within applicable federal scope; cross-border interactions | Fair-information program, meaningful consent, access/correction and appropriate breach handling | BLOCKED_APPROVAL |
| Alberta | Provincial private-sector scope and employee exceptions; outside-Canada service providers | Reasonable necessity, employee notice, access/correction, safeguards and RROSH incident review | BLOCKED_APPROVAL |
| British Columbia | Provincial private-sector and employee scope; decision-making record retention | Purpose/withdrawal, access/correction, section 35 schedules and breach guidance | BLOCKED_APPROVAL |
| Quebec | Private-sector scope; technology project, sensitive data, cross-border and biometric bank | PIA, officer/contact, express consent, alternative, biometric disclosure/60-day bank notice, incident register and rights | BLOCKED_APPROVAL |
| U.S. federal | FTC authority; sector/data-specific COPPA/FERPA/HIPAA and communications duties | Accurate claims, risk/vendor controls and separately reviewed sector scope | BLOCKED_APPROVAL |
| California | CCPA business thresholds/exemptions and role; employees; sensitive data/ADMT | Notice/rights, purpose and retention disclosure, service-provider terms, choices and applicable assessments | BLOCKED_APPROVAL |
| Illinois | BIPA private-entity/identifier coverage independent of generic consumer thresholds | Prior notice/release, public destruction schedule, restricted disclosure/no profit, safeguards | BLOCKED_APPROVAL |
| Texas | CUBI commercial capture and any applicable general privacy law | Consent, controlled disclosure, security and purpose-linked destruction; no generic BIPA copy | BLOCKED_APPROVAL |
| Washington | RCW 19.375 definitions/exclusions; commercial enrollment and any applicable health/privacy rules | Specific notice/consent and retention assessment; do not assume all CCTV is statutory biometric enrollment | BLOCKED_APPROVAL |
| Colorado | Biometric amendments and employee-specific rules; CPA coverage for other processing | Written biometric policy, applicable consent and request/retention handling; approved employment purpose | BLOCKED_APPROVAL |
| NYC | Covered commercial establishment and customer biometric collection | Required signage and sale/profit restrictions; consent alone is insufficient | BLOCKED_APPROVAL |
| Portland | Private facial recognition in public accommodations | Prohibition subject to defined exceptions; do not enable on generic consent | BLOCKED_APPROVAL |
| Other U.S. states/localities or regulated sectors | Launch locations/populations not yet selected; no comprehensive assessment performed | Expand official-source matrix and approve applicability before deployment | BLOCKED_APPROVAL |

General U.S. consumer laws and employment/biometric laws can overlap. The listed biometric assessments do not clear Texas, Washington or Colorado under all other state laws. Do not treat silence in this initial matrix as permission.

## India sources

| ID | Source / title | Version/date and reference | Implementation implication |
|---|---|---|---|
| IN-01 | India Code, Digital Personal Data Protection Act | Act 22 of 2023, 11 August 2023; [Act text](https://www.indiacode.nic.in/bitstream/123456789/22037/2/a2023-22.pdf), ss. 2–17, 44 | Distinguish fiduciary/processor, consent and specified legitimate uses; build rights and child safeguards toward applicable commencement |
| IN-02 | MeitY, commencement notification | G.S.R. 843(E), 13 November 2025; [Gazette](https://www.meity.gov.in/static/uploads/2025/11/c56ceae6c383460ca69577428d36828b.pdf) | Separate immediate, one-year and eighteen-month stages; dates detailed in India supplement |
| IN-03 | MeitY, Digital Personal Data Protection Rules | G.S.R. 846(E), 13 November 2025; [Rules](https://www.meity.gov.in/static/uploads/2025/11/53450e6e5dc0bfa85ebd78686cadad39.pdf), rr. 1, 3–16 | Stage notice, security, breach, retention, contacts, children and rights; do not implement a blanket 30-day deletion promise |
| IN-04 | MeitY, corrigenda to Rules | G.S.R. 892(E), 10 December 2025; [corrigenda](https://www.meity.gov.in/static/uploads/2025/12/3c7ebbae0e5456f493f486e6845df86b.pdf); ministry index posted later | Read corrected Gazette-publication wording and corrected clause range; publication-page dates are not commencement dates |
| IN-05 | CERT-In, directions under IT Act s.70B(6) and FAQ | 28 April 2022; [directions](https://www.cert-in.org.in/PDF/CERT-In_Directions_70B_28.04.2022.pdf), [May 2022 FAQ](https://www.cert-in.org.in/PDF/FAQs_on_CyberSecurityDirections_May2022.pdf) | Assess six-hour reportable incidents and 180-day ICT logs in India independently of DPDP staging |
| IN-06 | Government of India Gazette, IT reasonable-security/SPDI Rules, reproduced by WIPO Lex | G.S.R. 313(E), 11 April 2011; [official instrument reproduction](https://www.wipo.int/wipolex/en/text/494931), rr. 3–8 | Current sensitive-information/consent, privacy policy, one-month grievance, protection and transfer review; revisit when DPDP s.44(2) commences |

The original Gazette notification controls commencement: the India Code footnote broadly lists ss.35–43 in its first stage but the notification separately delays ss.36–37. This package follows G.S.R.843(E). No later changing notification was located in this check; this is not a guarantee against future amendments.

## Canada sources

| ID | Source / title | Version/date and reference | Implementation implication |
|---|---|---|---|
| CA-01 | Justice Canada, PIPEDA | S.C.2000 c.5; displayed current to 21 June 2026, amended 4 March 2025; [text](https://laws-lois.justice.gc.ca/eng/acts/P-8.6/FullText.html), ss.4,8,10.1–10.3, Schedule 1 | Determine coverage; fair-information principles; access and harm-based breach duties |
| CA-02 | OPC, provincial laws that may apply instead of PIPEDA; workplace privacy | Current guidance; [provincial scope](https://www.priv.gc.ca/en/privacy-topics/privacy-laws-in-canada/the-personal-information-protection-and-electronic-documents-act-pipeda/r_o_p/prov-pipeda/), [workplace](https://www.priv.gc.ca/en/privacy-topics/employers-and-employees/02_05_d_17) | Determine province, sector, cross-border and employee coverage instead of universal PIPEDA assumption |
| CA-03 | OPC, guidance for processing biometrics — businesses | Current guidance inspected; [guidance](https://www.priv.gc.ca/en/privacy-topics/health-genetic-and-other-body-information/biometrics/gd_bio_org-final/) | Necessity/proportionality, sensitive express consent, safeguards, alternatives and privacy risks |
| CA-04 | OIPC Alberta, PIPA guidance and breach requirements | Current regulator resources; [PIPA](https://oipc.ab.ca/resources/pipa/), [breach requirements](https://oipc.ab.ca/breach-notification/), [access guidance](https://oipc.ab.ca/resource/guidance-for-landlords-and-tenants/) | Assess employee distinction, 45-day access response, RROSH and notice without unreasonable delay; no blanket Canadian 72-hour clock |
| CA-05 | BC Laws, Personal Information Protection Act | SBC 2003 c.63, current consolidation inspected; [text](https://www.bclaws.gov.bc.ca/civix/document/id/complete/statreg/03063_01), ss.8,13,16,19,23–35 | Employee rules, withdrawal, access/correction and decision-record retention |
| CA-06 | Quebec Official Publisher, private-sector privacy Act | CQLR P-39.1, consolidation inspected; [text](https://www.legisquebec.gouv.qc.ca/en/document/cs/P-39.1?langcont=en), ss.3.1–3.8,12.1,14,17,18.3,23,27,32 | Officer, PIA, sensitive consent, incidents, outsourcing/transfers, portability and access |
| CA-07 | CAI, biometrics and Law 25 changes | Current regulator guidance; [biometrics](https://www.cai.gouv.qc.ca/protection-renseignements-personnels/sujets-et-domaines-dinteret/biometrie), [Law 25](https://www.cai.gouv.qc.ca/protection-renseignements-personnels/sujets-et-domaines-dinteret/principaux-changements-loi-25) | Express voluntary consent, non-biometric alternative, disclosure of use and 60-day advance biometric-bank disclosure |
| CA-08 | Justice Canada, Breach of Security Safeguards Regulations | SOR/2018-64, current text inspected; [text](https://laws-lois.justice.gc.ca/eng/regulations/SOR-2018-64/FullText.html), s.6 | Record all PIPEDA breaches for at least 24 months; do not store incident records publicly |
| CA-09 | OIPC BC, private-sector breach tools | Current resource inspected; [guidance](https://www.oipc.bc.ca/documents/guidance-documents/1361) | Evaluate guidance/voluntary reporting and overlapping duties; do not import public-sector legislation |

## United States sources

| ID | Source / title | Version/date and reference | Implementation implication |
|---|---|---|---|
| US-01 | FTC, biometric information and section 5 policy | 18 May 2023; [policy](https://www.ftc.gov/legal-library/browse/policy-statement-federal-trade-commission-biometric-information-section-5-federal-trade-commission) | Risk assessment, accurate claims, vendor monitoring and unfair/deceptive practice prevention |
| US-02 | California DOJ, CCPA guidance | Updated 28 August 2026; [guidance](https://oag.ca.gov/privacy/ccpa) | Assess business/employee coverage, notice, rights, sensitive information and sale/sharing |
| US-03 | CPPA, final regulations | Approved 22 September 2025, effective 1 January 2026; [rulemaking](https://cppa.ca.gov/regulations/ccpa_updates.html), [approved text](https://cppa.ca.gov/regulations/pdf/ccpa_updates_cyber_risk_admt_appr_text.pdf), ss.7121,7150–7157,7200 | Different risk-assessment, audit and ADMT applicability/timing; no claim that all dates are 2026 |
| US-04 | Illinois General Assembly, BIPA | 740 ILCS 14/15, current text inspected; [section 15](https://www.ilga.gov/legislation/ilcs/fulltext?DocName=074000140K15) | Prior written notice/release, destruction schedule, disclosure restrictions/no profit and reasonable protection |
| US-05 | Texas Legislature, CUBI | Business & Commerce Code 503.001, current text inspected; [chapter](https://statutes.capitol.texas.gov/Docs/BC/htm/BC.503.htm) | Commercial capture consent, protection, disclosure and destruction no later than one year after purpose expiry subject to statutory provisions |
| US-06 | Washington Legislature, biometric identifiers | RCW 19.375, current text inspected; [chapter](https://app.leg.wa.gov/RCW/default.aspx?cite=19.375&full=true) | Assess definitions/exclusions, enrollment, commercial purpose, disclosure and retention |
| US-07 | Colorado General Assembly, biometric identifiers/data | HB24-1130, enacted 2024, effective 1 July 2025; [bill and enacted act](https://www.leg.colorado.gov/bills/hb24-1130) | Written biometric policy/consent and employment-specific limits; general privacy thresholds alone do not clear biometrics |
| US-08 | NYC Council/DCWP, biometric identifiers | Local Law 3 of 2021 and current signage guidance; [enacted history](https://legistar.council.nyc.gov/LegislationDetail.aspx?GUID=070402C0-43F0-47AE-AA6E-DEF06CDF702A&ID=3704369&Options=ID%7CText%7C&Search=), [official signs](https://www.nyc.gov/site/dca/businesses/signs.page) | Covered customer-facing establishments need signage; sale/profit restrictions also apply |
| US-09 | Portland City Code, prohibition | Current code 34.10.030 inspected; [code](https://www.portland.gov/code/34/10/030) | Private face recognition in public accommodations prohibited subject to specified exceptions |
| US-10 | FTC / U.S. Education / HHS, sectoral scope | Current official resources inspected; [COPPA](https://www.ftc.gov/legal-library/browse/rules/childrens-online-privacy-protection-rule-coppa), [FERPA](https://studentprivacy.ed.gov/faq/what-ferpa), [HIPAA entities](https://www.hhs.gov/hipaa/for-professionals/covered-entities/index.html) | Review only when actual population/entity/data is covered; no school/health/child-service approval implied |

## Outstanding legal determinations

Named counsel must confirm the entity, customer roles, launch states/provinces/localities and regulated sectors; check subsequent laws/orders and actual statutory thresholds; approve grounds, worker/child safeguards, exact notice, response clocks, retention conflicts, cross-border/vendor terms and biometric prohibitions. Unknown facts remain BLOCKED_APPROVAL. Recheck at every material change and before launch; date-checked research is not a legal opinion.
