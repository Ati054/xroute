/** The route crossing mark is shared by the panel and public/favicon.svg. */
export function BrandMark() {
  return (
    <svg className="brand-mark" xmlns="http://www.w3.org/2000/svg" width="40" height="40"
      viewBox="0 0 40 40" fill="none" aria-hidden="true" focusable="false">
      <rect width="40" height="40" rx="10" fill="#15282E" />
      <path d="M12 15H15.5L17.7 17.75" stroke="#8FA6AD" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M21.3 22.25L23.5 25H28M25.2 22.3L28 25L25.2 27.7" stroke="#8FA6AD" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M12 25H15.5L23.5 15H28M25.2 12.3L28 15L25.2 17.7" stroke="#4ADEB8" strokeWidth="2.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function BrandName() {
  return <strong className="brand-name" dir="ltr"><span>X</span>Route</strong>;
}
